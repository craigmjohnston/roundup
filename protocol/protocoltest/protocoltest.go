// Package protocoltest drives a program that speaks the protocol, as the app would, over in-memory pipes.
package protocoltest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

const wait = 5 * time.Second

// Session is the app's end of a running program.
type Session struct {
	t     *testing.T
	in    *io.PipeWriter
	lines chan map[string]any
	done  chan error
}

// Start runs serve with the program's standard input and output connected to the session.
// serve returns when ctx ends or the session closes its input.
func Start(t *testing.T, serve func(ctx context.Context, in io.Reader, out io.Writer) error) *Session {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &Session{t: t, in: inW, lines: make(chan map[string]any, 64), done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); inW.Close() })
	go func() {
		s.done <- serve(ctx, inR, outW)
		outW.Close()
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("output line %q is not a JSON object: %v", sc.Text(), err)
				return
			}
			s.lines <- m
		}
		close(s.lines)
	}()
	return s
}

// Next returns the next line the program wrote.
func (s *Session) Next() map[string]any {
	s.t.Helper()
	select {
	case m, ok := <-s.lines:
		if !ok {
			s.t.Fatal("the program's output ended")
		}
		return m
	case <-time.After(wait):
		s.t.Fatalf("no line within %s", wait)
		return nil
	}
}

// Want returns the next line, which must have the given type.
func (s *Session) Want(typ string) map[string]any {
	s.t.Helper()
	m := s.Next()
	if m["type"] != typ {
		s.t.Fatalf("want a %s line, got %v", typ, m)
	}
	return m
}

// Send writes a request of the given type, as the app does.
func (s *Session) Send(typ string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, `{"type":"`+typ+`"}`+"\n"); err != nil {
		s.t.Fatal(err)
	}
}

// WriteRaw writes text to the program's input as it is.
func (s *Session) WriteRaw(text string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, text); err != nil {
		s.t.Fatal(err)
	}
}

// Close closes the program's input, as the app does when it quits.
func (s *Session) Close() { s.in.Close() }

// WantExit waits for the program to return, and checks that it returned want.
func (s *Session) WantExit(want error) {
	s.t.Helper()
	select {
	case err := <-s.done:
		if !errors.Is(err, want) {
			s.t.Fatalf("the program returned %v, want %v", err, want)
		}
	case <-time.After(wait):
		s.t.Fatal("the program did not return")
	}
}
