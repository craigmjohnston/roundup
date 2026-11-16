package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	t.Run("reads the sources", func(t *testing.T) {
		path := write(t, `{"sources": [{"command": ["/bin/plugin", "arg"]}, {"command": ["/bin/other"]}]}`)
		want := Config{Sources: []Source{{Command: []string{"/bin/plugin", "arg"}}, {Command: []string{"/bin/other"}}}}
		got, found, err := Load(path)
		if err != nil || !found {
			t.Fatalf("found %v, err %v", found, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("want %+v, got %+v", want, got)
		}
	})

	t.Run("reads a package source with its arguments", func(t *testing.T) {
		path := write(t, `{"sources": [{"package": "example.com/acme/tool@latest", "args": ["-workspace", "acme"]}]}`)
		want := Config{Sources: []Source{{Package: "example.com/acme/tool@latest", Args: []string{"-workspace", "acme"}}}}
		got, _, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("want %+v, got %+v", want, got)
		}
	})

	t.Run("an empty object has no sources", func(t *testing.T) {
		got, found, err := Load(write(t, `{}`))
		if err != nil || !found {
			t.Fatalf("found %v, err %v", found, err)
		}
		if len(got.Sources) != 0 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a missing file is not an error", func(t *testing.T) {
		_, found, err := Load(filepath.Join(t.TempDir(), "none.json"))
		if err != nil || found {
			t.Errorf("found %v, err %v", found, err)
		}
	})

	for name, content := range map[string]string{
		"a file that is not JSON":               `{`,
		"a source with no command":              `{"sources": [{}]}`,
		"a source with a command and a package": `{"sources": [{"command": ["/bin/plugin"], "package": "example.com/acme/tool@v1.0.0"}]}`,
		"a source with a command and args":      `{"sources": [{"command": ["/bin/plugin"], "args": ["x"]}]}`,
		"a field of the wrong type":             `{"sources": "acme"}`,
		"a slack section":                       `{"slack": {"workspace": "acme"}, "sources": [{"command": ["/bin/plugin"]}]}`,
		"a github section":                      `{"github": {}}`,
		"a github section that is empty":        `{"github": null}`,
	} {
		t.Run("fails on "+name, func(t *testing.T) {
			path := write(t, content)
			_, _, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Errorf("want an error naming %s, got %v", path, err)
			}
		})
	}

	t.Run("an invalid source error names the entry", func(t *testing.T) {
		path := write(t, `{"sources": [{"command": ["/bin/plugin"]}, {"command": ["/bin/plugin"], "package": "example.com/acme/tool@v1.0.0"}]}`)
		_, _, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "sources[1]") {
			t.Errorf("want an error naming sources[1], got %v", err)
		}
	})
}

func TestSourceName(t *testing.T) {
	if got := (Source{Command: []string{"/bin/plugin", "arg"}}).Name(); got != "/bin/plugin" {
		t.Errorf("got %s", got)
	}
	if got := (Source{Package: "example.com/acme/tool@latest"}).Name(); got != "example.com/acme/tool@latest" {
		t.Errorf("got %s", got)
	}
}

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct {
		name, xdg, want string
	}{
		{"uses ~/.config by default", "", filepath.Join(home, ".config", "roundup", "config.json")},
		{"uses XDG_CONFIG_HOME when set", "/xdg", filepath.Join("/xdg", "roundup", "config.json")},
		{"ignores a relative XDG_CONFIG_HOME", "rel", filepath.Join(home, ".config", "roundup", "config.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tc.xdg)
			path, err := DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			if path != tc.want {
				t.Errorf("want %s, got %s", tc.want, path)
			}
		})
	}
}
