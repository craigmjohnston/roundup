// Package config reads the app's configuration file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config says which sources the app runs.
type Config struct {
	// Sources are programs that speak the protocol in PROTOCOL.md.
	Sources []Source `json:"sources"`
}

// rejectedKeys are top-level settings the app refuses, because Slack and GitHub are plugins listed under sources.
var rejectedKeys = []string{"slack", "github"}

// Source is an external source program: either a command to run, or a Go package to install and run.
type Source struct {
	// Command is the program and its arguments.
	Command []string `json:"command"`
	// Package is a Go import path and version, such as example.com/org/repo/cmd/tool@v1.2.3, whose binary the app installs.
	Package string `json:"package"`
	// Args are the arguments for the installed binary.
	Args []string `json:"args"`
}

// Name says what the source runs, for logs.
func (s Source) Name() string {
	if s.Package != "" {
		return s.Package
	}
	return s.Command[0]
}

// DefaultPath is where the app looks for its configuration file: roundup/config.json in $XDG_CONFIG_HOME, or in ~/.config.
func DefaultPath() (string, error) {
	// os.UserConfigDir gives ~/Library/Application Support on macOS, which suits app data, not hand-edited files.
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding the home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "roundup", "config.json"), nil
}

// Load reads the file at path. A missing file is not an error: it gives an empty Config, and found is false.
func Load(path string) (cfg Config, found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(data, &keys)
	for _, key := range rejectedKeys {
		if _, ok := keys[key]; ok {
			return Config{}, false, fmt.Errorf("parsing %s: %q is not a setting; run its plugin as an entry in \"sources\" (see the README)", path, key)
		}
	}
	for i, s := range cfg.Sources {
		switch {
		case len(s.Command) > 0 && s.Package != "":
			return Config{}, false, fmt.Errorf("parsing %s: sources[%d] has both a command and a package; use one", path, i)
		case len(s.Command) == 0 && s.Package == "":
			return Config{}, false, fmt.Errorf("parsing %s: sources[%d] has no command or package", path, i)
		case len(s.Command) > 0 && len(s.Args) > 0:
			return Config{}, false, fmt.Errorf("parsing %s: sources[%d] has args with a command; put the arguments in the command", path, i)
		}
	}
	return cfg, true, nil
}
