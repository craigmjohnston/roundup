package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/craigmjohnston/roundup/task"
)

// Hooks are the parts of a plugin that are optional.
type Hooks struct {
	// Refresh polls now. When nil, refresh requests are ignored.
	Refresh func()
	// SignIn signs in, and may open a browser. When set, the hello offers sign-in.
	SignIn func(ctx context.Context) error
}

// Serve speaks the protocol on in and out for one source until in closes or ctx ends, and then returns nil.
// run supplies snapshots as the source's complete task list. When it returns task.ErrSignedOut, Serve tells the app, and runs it again after Hooks.SignIn succeeds.
// Any other error from run ends Serve with that error.
func Serve(ctx context.Context, in io.Reader, out io.Writer, info task.Info, run func(ctx context.Context, update func([]task.Task)) error, hooks Hooks) error {
	ctx, cancel := context.WithCancel(ctx)
	w := &writer{enc: json.NewEncoder(out)}
	if err := w.send(NewHello(info, hooks.SignIn != nil)); err != nil {
		cancel()
		return err
	}

	var wg sync.WaitGroup
	finish := func(err error) error {
		cancel()
		wg.Wait()
		return err
	}
	requests := readRequests(ctx, in)
	runDone := make(chan error, 1)
	signInDone := make(chan error, 1)
	running, signingIn := true, false
	wg.Go(func() { runDone <- run(ctx, w.tasks) })

	for {
		select {
		case <-ctx.Done():
			return finish(nil)
		case typ, ok := <-requests:
			if !ok {
				return finish(nil)
			}
			switch {
			case typ == "refresh" && hooks.Refresh != nil:
				hooks.Refresh()
			case typ == "sign_in" && hooks.SignIn != nil && !running && !signingIn:
				signingIn = true
				wg.Go(func() { signInDone <- hooks.SignIn(ctx) })
			}
		case err := <-runDone:
			running = false
			switch {
			case ctx.Err() != nil:
				return finish(nil)
			case errors.Is(err, task.ErrSignedOut) && hooks.SignIn != nil:
				if err := w.send(map[string]string{"type": "signed_out"}); err != nil {
					return finish(err)
				}
			case err != nil:
				return finish(err)
			default:
				return finish(nil)
			}
		case err := <-signInDone:
			signingIn = false
			if err != nil {
				if err := w.send(map[string]string{"type": "sign_in_failed", "error": err.Error()}); err != nil {
					return finish(err)
				}
				continue
			}
			if err := w.send(map[string]string{"type": "signed_in"}); err != nil {
				return finish(err)
			}
			running = true
			wg.Go(func() { runDone <- run(ctx, w.tasks) })
		}
	}
}

// ServeSource serves src, offering refresh and sign-in when src implements them.
func ServeSource(ctx context.Context, in io.Reader, out io.Writer, src task.Source) error {
	var hooks Hooks
	if r, ok := src.(task.Refresher); ok {
		hooks.Refresh = r.Refresh
	}
	if s, ok := src.(task.SignIn); ok {
		hooks.SignIn = s.SignIn
	}
	return Serve(ctx, in, out, src.Info(), src.Run, hooks)
}

type writer struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *writer) send(msg any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(msg); err != nil {
		return fmt.Errorf("writing to the app: %w", err)
	}
	return nil
}

func (w *writer) tasks(tasks []task.Task) {
	if tasks == nil {
		tasks = []task.Task{}
	}
	_ = w.send(struct {
		Type  string      `json:"type"`
		Tasks []task.Task `json:"tasks"`
	}{"tasks", tasks})
}

// readRequests delivers the type of each request line from the app, and closes the channel when in ends.
func readRequests(ctx context.Context, in io.Reader) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		r := bufio.NewReader(in)
		for {
			line, err := r.ReadBytes('\n')
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &head) == nil && head.Type != "" {
				select {
				case out <- head.Type:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}
