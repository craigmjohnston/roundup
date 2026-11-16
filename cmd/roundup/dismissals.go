package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

const dismissalsVersion = 1

type dismissal struct {
	Source  string    `json:"source"`
	Key     string    `json:"key"`
	Created time.Time `json:"created"`
}

// dismissals is the set of tasks the user has hidden, each matched by source, key and created time.
type dismissals struct {
	entries []dismissal
}

func (d dismissals) matches(e dismissal, t task.Task) bool {
	return e.Source == t.Source && e.Key == t.Key && e.Created.Equal(t.Created)
}

// Has reports whether the task is hidden.
func (d dismissals) Has(t task.Task) bool {
	return slices.ContainsFunc(d.entries, func(e dismissal) bool { return d.matches(e, t) })
}

// Add hides the task, and reports whether that changed anything.
func (d *dismissals) Add(t task.Task) bool {
	if d.Has(t) {
		return false
	}
	d.entries = append(d.entries, dismissal{Source: t.Source, Key: t.Key, Created: t.Created})
	return true
}

// Prune drops the source's dismissals that match none of the snapshot's tasks, and reports whether it dropped any.
func (d *dismissals) Prune(source string, snapshot []task.Task) bool {
	before := len(d.entries)
	d.entries = slices.DeleteFunc(d.entries, func(e dismissal) bool {
		if e.Source != source {
			return false
		}
		return !slices.ContainsFunc(snapshot, func(t task.Task) bool { return d.matches(e, t) })
	})
	return len(d.entries) != before
}

type dismissalsFile struct {
	Version   int         `json:"version"`
	Dismissed []dismissal `json:"dismissed"`
}

// loadDismissals reads the file at path; a missing, corrupt or wrong-version file gives an empty set.
func loadDismissals(path string) dismissals {
	data, err := os.ReadFile(path)
	if err != nil {
		return dismissals{}
	}
	var f dismissalsFile
	if json.Unmarshal(data, &f) != nil || f.Version != dismissalsVersion {
		return dismissals{}
	}
	return dismissals{entries: f.Dismissed}
}

// save writes the set to path through a temp file and a rename, so a reader never sees half a file.
func (d dismissals) save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(dismissalsFile{Version: dismissalsVersion, Dismissed: d.entries})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "dismissed-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// dismissalsPath is the real file, under the user's config directory.
func dismissalsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "roundup", "dismissed.json"), nil
}
