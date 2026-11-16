package external

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

const fakeEnv = "EXTERNAL_TEST_PLUGIN"

// TestMain turns the test binary into the fake plugin when fakeEnv is set.
func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) != "" {
		runFakePlugin()
		return
	}
	os.Exit(m.Run())
}

func emit(v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

func fakeTasks(title string) map[string]any {
	return map[string]any{"type": "tasks", "tasks": []task.Task{{
		Key:   "1",
		Title: title,
		Lead:  &task.Status{Symbol: "checkmark", Tone: task.ToneSuccess},
		Readouts: []task.Readout{
			{Symbol: "bubble.left.fill", Text: "2"},
			{Since: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Clock: task.ClockAgo},
		},
		Due: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
	}}}
}

// runFakePlugin behaves as the FAKE_* variables say; it stays up until its stdin closes.
func runFakePlugin() {
	runs := 1
	if path := os.Getenv("FAKE_RUNS_FILE"); path != "" {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintln(f, "start")
		f.Close()
		data, _ := os.ReadFile(path)
		runs = strings.Count(string(data), "start")
	}
	signIn := os.Getenv("FAKE_SIGN_IN") != ""
	emit(map[string]any{"type": "hello", "protocol": 1, "source": map[string]any{
		"id":        "fake",
		"name":      "Fake",
		"icon":      "mark",
		"icons":     map[string]any{"mark": map[string]any{"svg": "<svg/>", "template": true}},
		"sections":  []task.Section{{Name: "Extras", Order: 2, Fixed: true}},
		"canSignIn": signIn,
	}})

	switch {
	case os.Getenv("FAKE_MALFORMED") != "":
		fmt.Println("this is not json")
		fmt.Println(`{"type":"tasks","tasks":"nope"}`)
		fmt.Println(`{"type":"from_the_future","x":1}`)
		emit(fakeTasks("after the bad lines"))
	case os.Getenv("FAKE_SIGNED_OUT") != "":
		emit(map[string]any{"type": "signed_out"})
	case os.Getenv("FAKE_EXIT_FIRST_RUN") != "" && runs == 1:
		emit(fakeTasks("run 1"))
		return
	default:
		emit(fakeTasks(fmt.Sprint("run ", runs)))
	}

	polls := 0
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var m struct{ Type string }
		_ = json.Unmarshal(in.Bytes(), &m)
		switch m.Type {
		case "refresh":
			polls++
			fmt.Fprintln(os.Stderr, "fake plugin got a refresh")
			emit(fakeTasks(fmt.Sprint("refreshed ", polls)))
		case "sign_in":
			if os.Getenv("FAKE_SIGN_IN_FAILS") != "" {
				emit(map[string]any{"type": "sign_in_failed", "error": "wrong password"})
				continue
			}
			emit(map[string]any{"type": "signed_in"})
			emit(fakeTasks("signed in"))
		}
	}
}

type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logs) has(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// fake starts the fake plugin with the given environment.
func fake(t *testing.T, env map[string]string) (task.Source, *logs) {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	for k, v := range env {
		t.Setenv(k, v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	l := &logs{}
	src, err := open(ctx, []string{os.Args[0]}, options{helloTimeout: 10 * time.Second, restartDelay: 50 * time.Millisecond, logf: l.logf})
	if err != nil {
		t.Fatal(err)
	}
	return src, l
}

// updates is a stream of the snapshots a source delivers.
type updates chan []task.Task

func run(t *testing.T, src task.Source) (updates, chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	got := make(updates, 16)
	done := make(chan error, 1)
	go func() { done <- src.Run(ctx, func(tasks []task.Task) { got <- tasks }) }()
	return got, done
}

func (u updates) next(t *testing.T) []task.Task {
	t.Helper()
	select {
	case tasks := <-u:
		return tasks
	case <-time.After(10 * time.Second):
		t.Fatal("no snapshot arrived")
		return nil
	}
}

func (u updates) nextTitle(t *testing.T) string {
	t.Helper()
	tasks := u.next(t)
	if len(tasks) != 1 {
		t.Fatalf("want one task, got %+v", tasks)
	}
	return tasks[0].Title
}

func TestAFakePluginDescribesItselfAndDeliversTasks(t *testing.T) {
	src, l := fake(t, nil)

	wantInfo := task.Info{
		ID:       "fake",
		Name:     "Fake",
		Icon:     "mark",
		Icons:    map[string]task.Icon{"mark": {SVG: []byte("<svg/>"), Template: true}},
		Sections: []task.Section{{Name: "Extras", Order: 2, Fixed: true}},
	}
	if got := src.Info(); !reflect.DeepEqual(wantInfo, got) {
		t.Errorf("want %+v, got %+v", wantInfo, got)
	}
	if _, ok := src.(task.SignIn); ok {
		t.Error("a program without canSignIn offers sign-in")
	}

	got, _ := run(t, src)
	want := fakeTasks("run 1")["tasks"].([]task.Task)
	want[0].Source = "fake"
	if gotTasks := got.next(t); !reflect.DeepEqual(want, gotTasks) {
		t.Errorf("want %+v, got %+v", want, gotTasks)
	}

	t.Run("stderr is logged with the source id", func(t *testing.T) {
		src.(task.Refresher).Refresh()
		deadline := time.Now().Add(5 * time.Second)
		for !l.has("fake: fake plugin got a refresh") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !l.has("fake: fake plugin got a refresh") {
			t.Errorf("log lines: %q", l.lines)
		}
	})
}

func TestRefreshReachesThePlugin(t *testing.T) {
	src, _ := fake(t, nil)
	got, _ := run(t, src)
	if want, got := "run 1", got.nextTitle(t); want != got {
		t.Fatalf("want %q, got %q", want, got)
	}

	refresher, ok := src.(task.Refresher)
	if !ok {
		t.Fatal("source is not a Refresher")
	}
	refresher.Refresh()
	if want, title := "refreshed 1", got.nextTitle(t); want != title {
		t.Errorf("want %q, got %q", want, title)
	}
}

func TestSigningIn(t *testing.T) {
	t.Run("succeeds when the plugin answers signed_in", func(t *testing.T) {
		src, _ := fake(t, map[string]string{"FAKE_SIGN_IN": "1"})
		signer, ok := src.(task.SignIn)
		if !ok {
			t.Fatal("canSignIn did not give a SignIn source")
		}
		got, _ := run(t, src)
		got.next(t)
		if err := signer.SignIn(context.Background()); err != nil {
			t.Fatal(err)
		}
		if want, title := "signed in", got.nextTitle(t); want != title {
			t.Errorf("want %q, got %q", want, title)
		}
	})

	t.Run("fails with the plugin's message", func(t *testing.T) {
		src, _ := fake(t, map[string]string{"FAKE_SIGN_IN": "1", "FAKE_SIGN_IN_FAILS": "1"})
		err := src.(task.SignIn).SignIn(context.Background())
		if err == nil || err.Error() != "wrong password" {
			t.Errorf("want wrong password, got %v", err)
		}
	})

	t.Run("stops waiting when its context ends", func(t *testing.T) {
		src, _ := fake(t, map[string]string{"FAKE_SIGN_IN": "1", "FAKE_SIGNED_OUT": "1", "FAKE_SIGN_IN_FAILS": "1"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := src.(task.SignIn).SignIn(ctx); err == nil {
			t.Error("want an error")
		}
	})
}

func TestRunReturnsErrSignedOutWhenThePluginIsSignedOut(t *testing.T) {
	src, _ := fake(t, map[string]string{"FAKE_SIGN_IN": "1", "FAKE_SIGNED_OUT": "1"})
	_, done := run(t, src)
	select {
	case err := <-done:
		if !errors.Is(err, task.ErrSignedOut) {
			t.Errorf("want ErrSignedOut, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}

	t.Run("runs again once signed in", func(t *testing.T) {
		if err := src.(task.SignIn).SignIn(context.Background()); err != nil {
			t.Fatal(err)
		}
		got, _ := run(t, src)
		if want, title := "signed in", got.nextTitle(t); want != title {
			t.Errorf("want %q, got %q", want, title)
		}
	})
}

func TestThePluginIsStartedAgainAfterItExits(t *testing.T) {
	src, l := fake(t, map[string]string{"FAKE_EXIT_FIRST_RUN": "1", "FAKE_RUNS_FILE": t.TempDir() + "/runs"})
	got, _ := run(t, src)
	if want, title := "run 1", got.nextTitle(t); want != title {
		t.Fatalf("want %q, got %q", want, title)
	}
	if want, title := "run 2", got.nextTitle(t); want != title {
		t.Errorf("want %q, got %q", want, title)
	}
	if !l.has("exited") {
		t.Errorf("the exit was not logged: %q", l.lines)
	}
}

func TestBadLinesAreSkipped(t *testing.T) {
	src, l := fake(t, map[string]string{"FAKE_MALFORMED": "1"})
	got, _ := run(t, src)
	if want, title := "after the bad lines", got.nextTitle(t); want != title {
		t.Errorf("want %q, got %q", want, title)
	}
	if !l.has("malformed line") || !l.has("malformed tasks message") {
		t.Errorf("want both bad lines logged, got %q", l.lines)
	}
	if l.has("from_the_future") {
		t.Errorf("an unknown message type was logged: %q", l.lines)
	}
}

func TestOpenFails(t *testing.T) {
	t.Setenv(fakeEnv, "1")
	l := &logs{}
	opts := options{helloTimeout: 300 * time.Millisecond, restartDelay: time.Second, logf: l.logf}

	t.Run("when the program does not exist", func(t *testing.T) {
		if _, err := open(context.Background(), []string{"/nonexistent/plugin"}, opts); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("when there is no command", func(t *testing.T) {
		if _, err := open(context.Background(), nil, opts); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("when the program sends no hello in time", func(t *testing.T) {
		_, err := open(context.Background(), []string{"/bin/sleep", "30"}, opts)
		if err == nil || !strings.Contains(err.Error(), "no hello") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("when the program exits before its hello", func(t *testing.T) {
		if _, err := open(context.Background(), []string{"/usr/bin/true"}, opts); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("when the first line is not a hello", func(t *testing.T) {
		_, err := open(context.Background(), []string{"/bin/echo", `{"type":"tasks","tasks":[]}`}, opts)
		if err == nil || !strings.Contains(err.Error(), "not a hello") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("when the protocol is not one the app speaks", func(t *testing.T) {
		_, err := open(context.Background(), []string{"/bin/echo", `{"type":"hello","protocol":2,"source":{"id":"x"}}`}, opts)
		if err == nil || !strings.Contains(err.Error(), "protocol 2") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("when the hello has no source id", func(t *testing.T) {
		_, err := open(context.Background(), []string{"/bin/echo", `{"type":"hello","protocol":1,"source":{}}`}, opts)
		if err == nil || !strings.Contains(err.Error(), "no source id") {
			t.Errorf("got %v", err)
		}
	})
}
