// Package install builds plugin programs from Go packages with go install.
package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const recordFile = "installed.json"

// ErrNoGo means the go command was not found, so a package cannot be installed.
var ErrNoGo = errors.New("package sources need Go, which was not found on PATH")

// Installer installs packages into one directory and shares each install between callers that ask for the same package.
// A new Installer runs a latest install again, so make one per app start.
type Installer struct {
	dir       string
	goCommand string
	logf      func(format string, args ...any)

	// mu guards the record file and calls.
	mu    sync.Mutex
	calls map[string]*call
}

type call struct {
	done chan struct{}
	bin  string
	err  error
}

// DefaultDir is where the app keeps installed plugins: roundup/plugins/bin in the user's config directory.
func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the config directory: %w", err)
	}
	return filepath.Join(dir, "roundup", "plugins", "bin"), nil
}

// New returns an Installer that puts binaries and their record in dir, and finds go on PATH.
func New(dir string) *Installer {
	goCommand, _ := exec.LookPath("go")
	return &Installer{dir: dir, goCommand: goCommand, logf: log.Printf, calls: map[string]*call{}}
}

// Resolve returns the path of the binary for pkg, which is an import path and a version: example.com/org/repo/cmd/tool@v1.2.3.
// A pinned version installs when the binary is missing or came from another package or version. The version latest installs on every start,
// and an earlier binary is used if that fails.
func (i *Installer) Resolve(ctx context.Context, pkg string) (string, error) {
	i.mu.Lock()
	c, ok := i.calls[pkg]
	if !ok {
		c = &call{done: make(chan struct{})}
		i.calls[pkg] = c
	}
	i.mu.Unlock()
	if !ok {
		c.bin, c.err = i.resolve(ctx, pkg)
		close(c.done)
	}
	<-c.done
	return c.bin, c.err
}

func (i *Installer) resolve(ctx context.Context, pkg string) (string, error) {
	importPath, version, ok := strings.Cut(pkg, "@")
	if !ok || importPath == "" || version == "" {
		return "", fmt.Errorf("package %q is not <import path>@<version>", pkg)
	}
	if i.goCommand == "" {
		return "", ErrNoGo
	}
	name := path.Base(importPath)
	bin := filepath.Join(i.dir, name)
	_, statErr := os.Stat(bin)
	exists := statErr == nil

	if version != "latest" && exists && i.recorded(name) == pkg {
		return bin, nil
	}
	err := i.install(ctx, pkg, name)
	if err == nil {
		return bin, nil
	}
	if version == "latest" && exists {
		i.logf("installing %s failed, running the earlier install: %v", pkg, err)
		return bin, nil
	}
	return "", err
}

func (i *Installer) install(ctx context.Context, pkg, name string) error {
	if err := os.MkdirAll(i.dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", i.dir, err)
	}
	cmd := exec.CommandContext(ctx, i.goCommand, "install", pkg)
	cmd.Env = append(os.Environ(), "GOBIN="+i.dir, "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go install %s: %w: %s", pkg, err, strings.TrimSpace(string(out)))
	}
	return i.record(name, pkg)
}

func (i *Installer) recorded(name string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.readRecord()[name]
}

func (i *Installer) record(name, pkg string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	rec := i.readRecord()
	rec[name] = pkg
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(i.dir, recordFile), data, 0o644)
}

// readRecord returns what was installed, or an empty record when the file is missing or unreadable.
func (i *Installer) readRecord() map[string]string {
	rec := map[string]string{}
	data, err := os.ReadFile(filepath.Join(i.dir, recordFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		i.logf("reading %s: %v", recordFile, err)
	}
	_ = json.Unmarshal(data, &rec)
	return rec
}
