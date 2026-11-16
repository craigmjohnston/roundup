package install

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	fakeEnv     = "INSTALL_TEST_GO"
	fakeLogEnv  = "INSTALL_TEST_LOG"
	fakeFailEnv = "INSTALL_TEST_FAIL"
)

// TestMain turns the test binary into a fake go command when fakeEnv is set.
func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) != "" {
		runFakeGo()
		return
	}
	os.Exit(m.Run())
}

type goCall struct {
	Args   []string
	GOBIN  string
	GOWORK string
}

// runFakeGo records its call and writes the installed package's name and version as the binary.
func runFakeGo() {
	call := goCall{Args: os.Args[1:], GOBIN: os.Getenv("GOBIN"), GOWORK: os.Getenv("GOWORK")}
	data, _ := json.Marshal(call)
	f, _ := os.OpenFile(os.Getenv(fakeLogEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	fmt.Fprintf(f, "%s\n", data)
	f.Close()
	if os.Getenv(fakeFailEnv) != "" {
		fmt.Fprintln(os.Stderr, "module not found")
		os.Exit(1)
	}
	pkg := os.Args[2]
	importPath, _, _ := strings.Cut(pkg, "@")
	if err := os.WriteFile(filepath.Join(call.GOBIN, path.Base(importPath)), []byte(pkg), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type harness struct {
	t    *testing.T
	dir  string
	log  string
	logs []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{t: t, dir: filepath.Join(root, "plugins", "bin"), log: filepath.Join(root, "go.log")}
	t.Setenv(fakeEnv, "1")
	t.Setenv(fakeLogEnv, h.log)
	return h
}

// start makes an Installer for a new app start over the same directory.
func (h *harness) start() *Installer {
	i := New(h.dir)
	i.goCommand = os.Args[0]
	i.logf = func(format string, args ...any) { h.logs = append(h.logs, fmt.Sprintf(format, args...)) }
	return i
}

func (h *harness) calls() []goCall {
	h.t.Helper()
	data, err := os.ReadFile(h.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	var out []goCall
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var c goCall
		if err := json.Unmarshal(line, &c); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func (h *harness) resolve(i *Installer, pkg string) string {
	h.t.Helper()
	bin, err := i.Resolve(context.Background(), pkg)
	if err != nil {
		h.t.Fatalf("resolve %s: %v", pkg, err)
	}
	return bin
}

func (h *harness) installed(bin string) string {
	h.t.Helper()
	data, err := os.ReadFile(bin)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func TestResolve(t *testing.T) {
	const slack = "example.com/acme/roundup/plugins/slack/cmd/roundup-slack"

	t.Run("a pinned package installs once and is reused on later starts", func(t *testing.T) {
		h := newHarness(t)
		bin := h.resolve(h.start(), slack+"@v1.0.0")
		bin2 := h.resolve(h.start(), slack+"@v1.0.0")

		if want := filepath.Join(h.dir, "roundup-slack"); bin != want || bin2 != want {
			t.Errorf("want %s, got %s and %s", want, bin, bin2)
		}
		if got := len(h.calls()); got != 1 {
			t.Errorf("want 1 go install, got %d", got)
		}
	})

	t.Run("a pinned package runs go install with GOBIN and GOWORK set", func(t *testing.T) {
		h := newHarness(t)
		h.resolve(h.start(), slack+"@v1.0.0")

		want := goCall{Args: []string{"install", slack + "@v1.0.0"}, GOBIN: h.dir, GOWORK: "off"}
		got := h.calls()
		if len(got) != 1 || fmt.Sprint(want) != fmt.Sprint(got[0]) {
			t.Errorf("want %+v, got %+v", want, got)
		}
	})

	t.Run("a changed version installs again", func(t *testing.T) {
		h := newHarness(t)
		bin := h.resolve(h.start(), slack+"@v1.0.0")
		h.resolve(h.start(), slack+"@v1.1.0")

		if got := len(h.calls()); got != 2 {
			t.Errorf("want 2 go installs, got %d", got)
		}
		if want := slack + "@v1.1.0"; h.installed(bin) != want {
			t.Errorf("want %s installed, got %s", want, h.installed(bin))
		}
	})

	t.Run("a pinned package is installed again when its binary is missing", func(t *testing.T) {
		h := newHarness(t)
		bin := h.resolve(h.start(), slack+"@v1.0.0")
		if err := os.Remove(bin); err != nil {
			t.Fatal(err)
		}
		h.resolve(h.start(), slack+"@v1.0.0")

		if got := len(h.calls()); got != 2 {
			t.Errorf("want 2 go installs, got %d", got)
		}
	})

	t.Run("latest installs on every start", func(t *testing.T) {
		h := newHarness(t)
		h.resolve(h.start(), slack+"@latest")
		h.resolve(h.start(), slack+"@latest")
		h.resolve(h.start(), slack+"@latest")

		if got := len(h.calls()); got != 3 {
			t.Errorf("want 3 go installs, got %d", got)
		}
	})

	t.Run("a pinned version after latest installs, because latest is not recorded as pinned", func(t *testing.T) {
		h := newHarness(t)
		h.resolve(h.start(), slack+"@latest")
		h.resolve(h.start(), slack+"@v1.0.0")

		if got := len(h.calls()); got != 2 {
			t.Errorf("want 2 go installs, got %d", got)
		}
	})

	t.Run("sources asking for the same package in one start share one install", func(t *testing.T) {
		h := newHarness(t)
		i := h.start()
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() { h.resolve(i, slack+"@latest") })
		}
		wg.Wait()

		if got := len(h.calls()); got != 1 {
			t.Errorf("want 1 go install, got %d", got)
		}
	})

	t.Run("a failed latest install runs the binary from an earlier install", func(t *testing.T) {
		h := newHarness(t)
		bin := h.resolve(h.start(), slack+"@latest")
		t.Setenv(fakeFailEnv, "1")
		i := h.start()

		got, err := i.Resolve(context.Background(), slack+"@latest")
		if err != nil || got != bin {
			t.Fatalf("want %s, got %s, err %v", bin, got, err)
		}
		if len(h.logs) != 1 || !strings.Contains(h.logs[0], "module not found") {
			t.Errorf("want one log line with the go output, got %q", h.logs)
		}
	})

	t.Run("a failed install with no binary is an error", func(t *testing.T) {
		h := newHarness(t)
		t.Setenv(fakeFailEnv, "1")

		_, err := h.start().Resolve(context.Background(), slack+"@latest")
		if err == nil || !strings.Contains(err.Error(), "module not found") {
			t.Errorf("want an error with the go output, got %v", err)
		}
	})

	t.Run("a failed pinned install does not run another version", func(t *testing.T) {
		h := newHarness(t)
		h.resolve(h.start(), slack+"@v1.0.0")
		t.Setenv(fakeFailEnv, "1")

		if _, err := h.start().Resolve(context.Background(), slack+"@v1.1.0"); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("the binary is named after the last element of the import path", func(t *testing.T) {
		h := newHarness(t)
		bin := h.resolve(h.start(), "example.com/acme/tool@v2.0.0")
		if want := filepath.Join(h.dir, "tool"); bin != want {
			t.Errorf("want %s, got %s", want, bin)
		}
	})

	t.Run("a package without a version is an error", func(t *testing.T) {
		h := newHarness(t)
		for _, pkg := range []string{"example.com/acme/tool", "example.com/acme/tool@", "@v1"} {
			if _, err := h.start().Resolve(context.Background(), pkg); err == nil {
				t.Errorf("%q: want an error", pkg)
			}
		}
	})

	t.Run("without go on PATH it says Go is needed", func(t *testing.T) {
		h := newHarness(t)
		i := h.start()
		i.goCommand = ""

		_, err := i.Resolve(context.Background(), slack+"@latest")
		if err != ErrNoGo || !strings.Contains(err.Error(), "need Go") {
			t.Errorf("want ErrNoGo, got %v", err)
		}
	})
}

func TestNewFindsGoOnPath(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if got, want := New(t.TempDir()).goCommand, filepath.Join(bin, "go"); got != want {
		t.Errorf("want %s, got %s", want, got)
	}
	t.Setenv("PATH", t.TempDir())
	if got := New(t.TempDir()).goCommand; got != "" {
		t.Errorf("want no go command, got %s", got)
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, filepath.Join("roundup", "plugins", "bin")) {
		t.Errorf("got %s", dir)
	}
}
