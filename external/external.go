// Package external runs a task source as a separate program that speaks the protocol in PROTOCOL.md.
package external

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/craigmjohnston/roundup/protocol"
	"github.com/craigmjohnston/roundup/task"
)

const (
	defaultHelloTimeout = 10 * time.Second
	defaultRestartDelay = 30 * time.Second
)

// Source is a task source backed by a child program, which is restarted when it exits.
type Source struct {
	command      []string
	helloTimeout time.Duration
	restartDelay time.Duration
	logf         func(format string, args ...any)

	info task.Info
	id   atomic.Pointer[string]

	// writeMu serialises writes to the program's stdin.
	writeMu sync.Mutex
	stdin   io.Writer

	// mu guards the fields below.
	mu        sync.Mutex
	tasks     []task.Task
	haveTasks bool
	signedOut bool
	update    func([]task.Task)
	runDone   chan error
	signIn    chan error
}

// signInSource is a Source whose program offers to sign in.
type signInSource struct{ *Source }

var (
	_ task.Source    = (*Source)(nil)
	_ task.Refresher = (*Source)(nil)
	_ task.SignIn    = signInSource{}
)

// Open starts the program and waits for its hello, so that Info is known before Run.
// The program is restarted after it exits until ctx ends. The result implements task.SignIn only when the program offers sign-in.
func Open(ctx context.Context, command []string) (task.Source, error) {
	return open(ctx, command, options{helloTimeout: defaultHelloTimeout, restartDelay: defaultRestartDelay, logf: log.Printf})
}

type options struct {
	helloTimeout time.Duration
	restartDelay time.Duration
	logf         func(format string, args ...any)
}

func open(ctx context.Context, command []string, opts options) (task.Source, error) {
	if len(command) == 0 {
		return nil, errors.New("no command")
	}
	s := &Source{command: command, helloTimeout: opts.helloTimeout, restartDelay: opts.restartDelay, logf: opts.logf}
	name := filepath.Base(command[0])
	s.id.Store(&name)
	p, hello, err := s.start(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	s.info = hello.Source.Info()
	if s.info.ID == "" {
		p.kill()
		_ = p.cmd.Wait()
		return nil, fmt.Errorf("%s: hello has no source id", name)
	}
	id := s.info.ID
	s.id.Store(&id)
	s.attach(p)
	go s.supervise(ctx, p)
	if hello.Source.CanSignIn {
		return signInSource{s}, nil
	}
	return s, nil
}

// Info is the description from the program's first hello.
func (s *Source) Info() task.Info { return s.info }

// Run delivers the program's snapshots until ctx ends, or returns task.ErrSignedOut when the program says it is signed out.
func (s *Source) Run(ctx context.Context, update func([]task.Task)) error {
	s.mu.Lock()
	if s.signedOut {
		s.mu.Unlock()
		return task.ErrSignedOut
	}
	done := make(chan error, 1)
	s.update, s.runDone = update, done
	if s.haveTasks {
		update(s.tasks)
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.update, s.runDone = nil, nil
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return err
	}
}

// Refresh asks the program to poll now.
func (s *Source) Refresh() {
	if err := s.send(map[string]string{"type": "refresh"}); err != nil {
		s.logf("%s: refresh: %v", s.name(), err)
	}
}

// SignIn asks the program to sign in and waits for its answer.
func (s signInSource) SignIn(ctx context.Context) error {
	s.mu.Lock()
	if s.signIn != nil {
		s.mu.Unlock()
		return errors.New("sign-in already in progress")
	}
	answer := make(chan error, 1)
	s.signIn = answer
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.signIn = nil
		s.mu.Unlock()
	}()

	if err := s.send(map[string]string{"type": "sign_in"}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-answer:
		return err
	}
}

func (s *Source) name() string { return *s.id.Load() }

func (s *Source) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.stdin == nil {
		return errors.New("the program is not running")
	}
	_, err = s.stdin.Write(append(data, '\n'))
	return err
}

type proc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func (p *proc) kill() { _ = p.cmd.Process.Kill() }

// start launches the program and reads its hello.
func (s *Source) start(ctx context.Context) (*proc, protocol.Hello, error) {
	cmd := exec.CommandContext(ctx, s.command[0], s.command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, protocol.Hello{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, protocol.Hello{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, protocol.Hello{}, err
	}
	if err := cmd.Start(); err != nil {
		return nil, protocol.Hello{}, err
	}
	go s.logLines(stderr)
	p := &proc{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}

	type result struct {
		hello protocol.Hello
		err   error
	}
	got := make(chan result, 1)
	go func() {
		h, err := readHello(p.stdout)
		got <- result{h, err}
	}()
	select {
	case r := <-got:
		if r.err != nil {
			p.kill()
			_ = cmd.Wait()
			return nil, protocol.Hello{}, r.err
		}
		return p, r.hello, nil
	case <-time.After(s.helloTimeout):
		p.kill()
		_ = cmd.Wait()
		return nil, protocol.Hello{}, fmt.Errorf("no hello within %s", s.helloTimeout)
	}
}

func readHello(r *bufio.Reader) (protocol.Hello, error) {
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return protocol.Hello{}, fmt.Errorf("the program exited before its hello: %w", err)
	}
	var h protocol.Hello
	if err := json.Unmarshal(line, &h); err != nil || h.Type != "hello" {
		return protocol.Hello{}, errors.New("the first line is not a hello")
	}
	if h.Protocol != protocol.Version {
		return protocol.Hello{}, fmt.Errorf("the program speaks protocol %d, and the app speaks %d", h.Protocol, protocol.Version)
	}
	return h, nil
}

func (s *Source) logLines(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		s.logf("%s: %s", s.name(), sc.Text())
	}
}

func (s *Source) attach(p *proc) {
	s.writeMu.Lock()
	s.stdin = p.stdin
	s.writeMu.Unlock()
}

func (s *Source) detach() {
	s.writeMu.Lock()
	s.stdin = nil
	s.writeMu.Unlock()
	s.mu.Lock()
	if s.signIn != nil {
		offer(s.signIn, errors.New("the program exited"))
	}
	s.signedOut = false
	s.mu.Unlock()
}

// supervise reads the program's messages and starts it again, after the restart delay, each time it exits.
func (s *Source) supervise(ctx context.Context, p *proc) {
	for {
		s.read(p)
		_ = p.stdin.Close()
		err := p.cmd.Wait()
		s.detach()
		if ctx.Err() != nil {
			return
		}
		s.logf("%s: exited (%v); starting it again in %s", s.name(), err, s.restartDelay)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.restartDelay):
			}
			next, _, err := s.start(ctx)
			if err != nil {
				s.logf("%s: %v; trying again in %s", s.name(), err, s.restartDelay)
				continue
			}
			p = next
			s.attach(p)
			break
		}
	}
}

func (s *Source) read(p *proc) {
	for {
		line, err := p.stdout.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			s.dispatch(line)
		}
		if err != nil {
			return
		}
	}
}

func (s *Source) dispatch(line []byte) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		s.logf("%s: skipping a malformed line: %v", s.name(), err)
		return
	}
	switch head.Type {
	case "tasks":
		var m struct {
			Tasks []task.Task `json:"tasks"`
		}
		if err := json.Unmarshal(line, &m); err != nil {
			s.logf("%s: skipping a malformed tasks message: %v", s.name(), err)
			return
		}
		s.setTasks(m.Tasks)
	case "signed_out":
		s.mu.Lock()
		s.signedOut = true
		s.tasks, s.haveTasks = nil, false
		if s.runDone != nil {
			offer(s.runDone, task.ErrSignedOut)
		}
		s.mu.Unlock()
	case "signed_in":
		s.answerSignIn(nil, func() { s.signedOut = false })
	case "sign_in_failed":
		var m struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(line, &m)
		if m.Error == "" {
			m.Error = "sign-in failed"
		}
		s.answerSignIn(errors.New(m.Error), func() {})
	}
}

func (s *Source) setTasks(tasks []task.Task) {
	for i := range tasks {
		tasks[i].Source = s.info.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks, s.haveTasks, s.signedOut = tasks, true, false
	if s.update != nil {
		s.update(tasks)
	}
}

func (s *Source) answerSignIn(err error, apply func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	apply()
	if s.signIn != nil {
		offer(s.signIn, err)
	}
}

// offer sends to a one-slot channel without blocking; a second answer to the same wait is dropped.
func offer(ch chan error, err error) {
	select {
	case ch <- err:
	default:
	}
}
