package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func tk(source, key string, created time.Time) task.Task {
	return task.Task{Source: source, Key: key, Created: created}
}

func TestDismissals_Matching(t *testing.T) {
	t.Run("hides a task with the same source, key and created time", func(t *testing.T) {
		var d dismissals
		d.Add(tk("x", "a", day(1)))
		if !d.Has(tk("x", "a", day(1))) {
			t.Error("want task hidden")
		}
	})

	t.Run("shows the task again when created changes", func(t *testing.T) {
		var d dismissals
		d.Add(tk("x", "a", day(1)))
		if d.Has(tk("x", "a", day(2))) {
			t.Error("want task shown")
		}
	})

	t.Run("leaves the same key in another source alone", func(t *testing.T) {
		var d dismissals
		d.Add(tk("x", "a", day(1)))
		if d.Has(tk("y", "a", day(1))) {
			t.Error("want task shown")
		}
	})

	t.Run("adding twice records once", func(t *testing.T) {
		var d dismissals
		d.Add(tk("x", "a", day(1)))
		if d.Add(tk("x", "a", day(1))) {
			t.Error("want second add to change nothing")
		}
		if want, got := 1, len(d.entries); want != got {
			t.Errorf("entries: want %d, got %d", want, got)
		}
	})
}

func TestDismissals_Prune(t *testing.T) {
	t.Run("drops a source's entries missing from its snapshot and keeps the rest", func(t *testing.T) {
		var d dismissals
		d.Add(tk("x", "kept", day(1)))
		d.Add(tk("x", "gone", day(1)))
		d.Add(tk("x", "changed", day(1)))
		changed := d.Prune("x", []task.Task{tk("x", "kept", day(1)), tk("x", "changed", day(2))})
		if !changed {
			t.Error("want prune to report a change")
		}
		if want, got := []string{"kept"}, entryKeys(d); !slices.Equal(want, got) {
			t.Errorf("entries: want %v, got %v", want, got)
		}
	})

	t.Run("leaves other sources' entries alone", func(t *testing.T) {
		var d dismissals
		d.Add(tk("y", "a", day(1)))
		if d.Prune("x", nil) {
			t.Error("want no change")
		}
		if want, got := []string{"a"}, entryKeys(d); !slices.Equal(want, got) {
			t.Errorf("entries: want %v, got %v", want, got)
		}
	})
}

func entryKeys(d dismissals) []string {
	var ks []string
	for _, e := range d.entries {
		ks = append(ks, e.Key)
	}
	return ks
}

func TestDismissals_Persistence(t *testing.T) {
	t.Run("a saved set loads back, created times included", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cfg", "dismissed.json")
		var d dismissals
		d.Add(tk("x", "a", day(1).Add(123456789*time.Nanosecond)))
		d.Add(tk("y", "b", day(2)))
		if err := d.save(path); err != nil {
			t.Fatal(err)
		}
		got := loadDismissals(path)
		if !got.Has(tk("x", "a", day(1).Add(123456789*time.Nanosecond))) || !got.Has(tk("y", "b", day(2))) {
			t.Errorf("want both entries back, got %v", got.entries)
		}
		if want, got := 2, len(got.entries); want != got {
			t.Errorf("entries: want %d, got %d", want, got)
		}
	})

	t.Run("the file is mode 0600 and its directory 0700", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cfg")
		path := filepath.Join(dir, "dismissed.json")
		if err := (dismissals{}).save(path); err != nil {
			t.Fatal(err)
		}
		assertMode(t, path, 0o600)
		assertMode(t, dir, 0o700)
	})

	t.Run("writing leaves no temp file behind", func(t *testing.T) {
		dir := t.TempDir()
		var d dismissals
		d.Add(tk("x", "a", day(1)))
		for range 2 {
			if err := d.save(filepath.Join(dir, "dismissed.json")); err != nil {
				t.Fatal(err)
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if want, got := 1, len(entries); want != got {
			t.Errorf("files: want %d, got %d", want, got)
		}
	})

	t.Run("a failed write keeps the old file and leaves no temp file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "dismissed.json")
		var d dismissals
		d.Add(tk("x", "a", day(1)))
		if err := d.save(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := d.save(filepath.Join(dir, "sub")); err == nil {
			t.Fatal("want error renaming over a directory")
		}
		if !loadDismissals(path).Has(tk("x", "a", day(1))) {
			t.Error("want old file intact")
		}
		entries, _ := os.ReadDir(dir)
		if want, got := 2, len(entries); want != got {
			t.Errorf("files: want %d, got %d", want, got)
		}
	})

	t.Run("a missing file means nothing dismissed", func(t *testing.T) {
		got := loadDismissals(filepath.Join(t.TempDir(), "none.json"))
		if len(got.entries) != 0 {
			t.Errorf("want none, got %v", got.entries)
		}
	})

	t.Run("a corrupt file means nothing dismissed", func(t *testing.T) {
		assertLoadsEmpty(t, "{not json")
	})

	t.Run("a wrong-version file means nothing dismissed", func(t *testing.T) {
		assertLoadsEmpty(t, `{"version":99,"dismissed":[{"source":"x","key":"a","created":"2026-11-01T12:00:00Z"}]}`)
	})
}

func assertLoadsEmpty(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dismissed.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadDismissals(path); len(got.entries) != 0 {
		t.Errorf("want none, got %v", got.entries)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); want != got {
		t.Errorf("%s mode: want %v, got %v", path, want, got)
	}
}

func TestDismissalsPath(t *testing.T) {
	t.Run("is under the user config directory", func(t *testing.T) {
		dir, err := os.UserConfigDir()
		if err != nil {
			t.Skip(err)
		}
		got, err := dismissalsPath()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, "roundup", "dismissed.json"); want != got {
			t.Errorf("path: want %q, got %q", want, got)
		}
	})
}

func writeBlocker(path string) error {
	return os.WriteFile(path, nil, 0o600)
}
