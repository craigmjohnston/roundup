package protocol_test

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/external"
	"github.com/craigmjohnston/roundup/protocol"
	"github.com/craigmjohnston/roundup/protocol/protocoltest"
	"github.com/craigmjohnston/roundup/task"
)

const interopEnv = "PROTOCOL_TEST_PLUGIN"

var testInfo = task.Info{
	ID:       "example",
	Name:     "Example",
	Icon:     "mark",
	Icons:    map[string]task.Icon{"mark": {SVG: []byte("<svg/>"), Template: true}},
	Sections: []task.Section{{Name: "Releases", Order: 1, Fixed: true}},
}

// TestMain turns the test binary into a plugin built on Serve when interopEnv is set.
func TestMain(m *testing.M) {
	if os.Getenv(interopEnv) != "" {
		src := &fakeSource{signedOut: true}
		if err := protocol.ServeSource(context.Background(), os.Stdin, os.Stdout, src); err != nil {
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

type fakeSource struct{ signedOut bool }

func (s *fakeSource) Info() task.Info { return testInfo }

func (s *fakeSource) Run(ctx context.Context, update func([]task.Task)) error {
	if s.signedOut {
		return task.ErrSignedOut
	}
	update([]task.Task{{Key: "1", Title: "first"}})
	<-ctx.Done()
	return nil
}

func (s *fakeSource) SignIn(context.Context) error {
	s.signedOut = false
	return nil
}

func (s *fakeSource) Refresh() {}

func serve(t *testing.T, info task.Info, run func(ctx context.Context, update func([]task.Task)) error, hooks protocol.Hooks) *protocoltest.Session {
	t.Helper()
	return protocoltest.Start(t, func(ctx context.Context, in io.Reader, out io.Writer) error {
		return protocol.Serve(ctx, in, out, info, run, hooks)
	})
}

func idle(ctx context.Context, _ func([]task.Task)) error { <-ctx.Done(); return nil }

func TestServe_Hello(t *testing.T) {
	t.Run("describes the source", func(t *testing.T) {
		s := serve(t, testInfo, idle, protocol.Hooks{})
		h := s.Want("hello")
		want := map[string]any{
			"type":     "hello",
			"protocol": float64(1),
			"source": map[string]any{
				"id":        "example",
				"name":      "Example",
				"icon":      "mark",
				"icons":     map[string]any{"mark": map[string]any{"svg": "<svg/>", "template": true}},
				"sections":  []any{map[string]any{"name": "Releases", "order": float64(1), "fixed": true}},
				"canSignIn": false,
			},
		}
		if !reflect.DeepEqual(want, h) {
			t.Errorf("want %v, got %v", want, h)
		}
	})

	t.Run("offers sign-in only when there is a hook for it", func(t *testing.T) {
		s := serve(t, testInfo, idle, protocol.Hooks{SignIn: func(context.Context) error { return nil }})
		src := s.Want("hello")["source"].(map[string]any)
		if src["canSignIn"] != true {
			t.Errorf("want canSignIn, got %v", src)
		}
	})
}

func TestServe_Tasks(t *testing.T) {
	t.Run("writes each snapshot, with an empty list as an array", func(t *testing.T) {
		run := func(ctx context.Context, update func([]task.Task)) error {
			update([]task.Task{{Key: "a", Title: "first", Due: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)}})
			update(nil)
			<-ctx.Done()
			return nil
		}
		s := serve(t, testInfo, run, protocol.Hooks{})
		s.Want("hello")
		first := s.Want("tasks")["tasks"].([]any)[0].(map[string]any)
		if want := map[string]any{"key": "a", "source": "", "title": "first", "due": "2026-02-03T00:00:00Z"}; first["key"] != want["key"] || first["due"] != want["due"] || first["title"] != want["title"] {
			t.Errorf("want %v, got %v", want, first)
		}
		empty := s.Want("tasks")["tasks"]
		if got, ok := empty.([]any); !ok || len(got) != 0 {
			t.Errorf("want an empty array, got %#v", empty)
		}
	})
}

func TestServe_Requests(t *testing.T) {
	t.Run("a refresh calls the hook", func(t *testing.T) {
		refreshed := make(chan struct{}, 1)
		s := serve(t, testInfo, idle, protocol.Hooks{Refresh: func() { refreshed <- struct{}{} }})
		s.Want("hello")
		s.Send("refresh")
		select {
		case <-refreshed:
		case <-time.After(5 * time.Second):
			t.Fatal("the refresh hook was not called")
		}
	})

	t.Run("a refresh with no hook, a request from the future and a malformed line change nothing", func(t *testing.T) {
		s := serve(t, testInfo, idle, protocol.Hooks{})
		s.Want("hello")
		s.Send("refresh")
		s.Send("from_the_future")
		s.WriteRaw("not json\n")
		s.Close()
		s.WantExit(nil)
	})
}

func TestServe_Exit(t *testing.T) {
	t.Run("exits cleanly when stdin closes, ending run", func(t *testing.T) {
		ended := make(chan struct{})
		run := func(ctx context.Context, _ func([]task.Task)) error { <-ctx.Done(); close(ended); return nil }
		s := serve(t, testInfo, run, protocol.Hooks{})
		s.Want("hello")
		s.Close()
		s.WantExit(nil)
		select {
		case <-ended:
		default:
			t.Error("run was still going when Serve returned")
		}
	})

	t.Run("returns an error from run", func(t *testing.T) {
		boom := errors.New("boom")
		s := serve(t, testInfo, func(context.Context, func([]task.Task)) error { return boom }, protocol.Hooks{})
		s.Want("hello")
		s.WantExit(boom)
	})

	t.Run("returns signed out when there is no way to sign in", func(t *testing.T) {
		s := serve(t, testInfo, func(context.Context, func([]task.Task)) error { return task.ErrSignedOut }, protocol.Hooks{})
		s.Want("hello")
		s.WantExit(task.ErrSignedOut)
	})
}

func TestServe_SignIn(t *testing.T) {
	t.Run("signs in on request and runs again", func(t *testing.T) {
		signedIn := false
		run := func(ctx context.Context, update func([]task.Task)) error {
			if !signedIn {
				return task.ErrSignedOut
			}
			update([]task.Task{{Key: "1"}})
			<-ctx.Done()
			return nil
		}
		s := serve(t, testInfo, run, protocol.Hooks{SignIn: func(context.Context) error { signedIn = true; return nil }})
		s.Want("hello")
		s.Want("signed_out")
		s.Send("sign_in")
		s.Want("signed_in")
		s.Want("tasks")
	})

	t.Run("reports a failed sign-in and accepts another", func(t *testing.T) {
		attempts := 0
		signedIn := false
		hooks := protocol.Hooks{SignIn: func(context.Context) error {
			attempts++
			if attempts == 1 {
				return errors.New("wrong password")
			}
			signedIn = true
			return nil
		}}
		s := serve(t, testInfo, func(ctx context.Context, update func([]task.Task)) error {
			if !signedIn {
				return task.ErrSignedOut
			}
			update(nil)
			<-ctx.Done()
			return nil
		}, hooks)
		s.Want("hello")
		s.Want("signed_out")
		s.Send("sign_in")
		if got := s.Want("sign_in_failed")["error"]; got != "wrong password" {
			t.Errorf("want the sign-in error, got %v", got)
		}
		s.Send("sign_in")
		s.Want("signed_in")
		s.Want("tasks")
	})

	t.Run("ignores sign_in while signed in", func(t *testing.T) {
		called := make(chan struct{}, 1)
		s := serve(t, testInfo, idle, protocol.Hooks{SignIn: func(context.Context) error { called <- struct{}{}; return nil }})
		s.Want("hello")
		s.Send("sign_in")
		s.Send("refresh")
		s.Close()
		s.WantExit(nil)
		select {
		case <-called:
			t.Error("SignIn was called while signed in")
		default:
		}
	})

	t.Run("cancels a sign-in that is still going when stdin closes", func(t *testing.T) {
		started := make(chan struct{})
		run := func(context.Context, func([]task.Task)) error { return task.ErrSignedOut }
		s := serve(t, testInfo, run, protocol.Hooks{SignIn: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}})
		s.Want("hello")
		s.Want("signed_out")
		s.Send("sign_in")
		<-started
		s.Close()
		s.WantExit(nil)
	})
}

// TestServeSource_AgainstTheRunner runs a Serve-based program under the app's runner.
func TestServeSource_AgainstTheRunner(t *testing.T) {
	t.Setenv(interopEnv, "1")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	src, err := external.Open(ctx, []string{os.Args[0]})
	if err != nil {
		t.Fatal(err)
	}
	if want, got := testInfo, src.Info(); !reflect.DeepEqual(want, got) {
		t.Errorf("want %+v, got %+v", want, got)
	}
	signer, ok := src.(task.SignIn)
	if !ok {
		t.Fatal("the source does not offer sign-in")
	}
	if err := src.Run(ctx, func([]task.Task) {}); !errors.Is(err, task.ErrSignedOut) {
		t.Fatalf("want signed out, got %v", err)
	}
	if err := signer.SignIn(ctx); err != nil {
		t.Fatal(err)
	}
	got := make(chan []task.Task, 1)
	go src.Run(ctx, func(tasks []task.Task) { got <- tasks })
	select {
	case tasks := <-got:
		if len(tasks) != 1 || tasks[0].Source != "example" || tasks[0].Title != "first" {
			t.Errorf("unexpected tasks %+v", tasks)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no tasks after signing in")
	}
}
