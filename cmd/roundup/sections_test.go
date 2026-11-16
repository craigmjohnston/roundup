package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/craigmjohnston/roundup/task"
)

func sec(source, key, section string, created int) task.Task {
	return task.Task{Source: source, Key: key, Section: section, Created: day(created)}
}

func sectionLabels(s *state) []string {
	var out []string
	for _, r := range append(s.scrollingRows(), s.fixedRows()...) {
		if r.heading != "" {
			out = append(out, "## "+r.heading)
		} else {
			out = append(out, r.task.Key)
		}
	}
	return out
}

func assertSections(t *testing.T, s *state, want ...string) {
	t.Helper()
	if got := sectionLabels(s); !slices.Equal(want, got) {
		t.Errorf("sections: want %v, got %v", want, got)
	}
}

func TestStateSections(t *testing.T) {
	t.Run("keeps section tasks out of the main list and lists each section by name, newest first", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{
			{Key: "m2", Source: "x", Due: day(5)},
			{Key: "m1", Source: "x", Due: day(2)},
			sec("x", "z-old", "Zeta", 1),
			sec("x", "z-new", "Zeta", 9),
			sec("x", "a-tie-b", "Alpha", 4),
			sec("x", "a-tie-a", "Alpha", 4),
		})
		assertKeys(t, &s, "m1", "m2")
		assertSections(t, &s, "## Alpha", "a-tie-a", "a-tie-b", "## Zeta", "z-new", "z-old")
	})

	t.Run("gathers a section's tasks from several sources", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{sec("x", "a", "Mine", 1)})
		s.Snapshot("y", []task.Task{sec("y", "b", "Mine", 2)})
		assertSections(t, &s, "## Mine", "b", "a")
	})

	t.Run("leaves out a section with no tasks", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{{Key: "m", Source: "x"}})
		assertKeys(t, &s, "m")
		assertSections(t, &s)
	})

	t.Run("counts main-list tasks only", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{{Key: "m", Source: "x"}, sec("x", "a", "Mine", 1), sec("x", "b", "Mine", 2)})
		if want, got := "1", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
		if want, got := 1, len(s.tasks); want != got {
			t.Errorf("count: want %d, got %d", want, got)
		}
	})

	t.Run("shows the main area as empty while sections have tasks", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{sec("x", "a", "Mine", 1)})
		assertSections(t, &s, "## Mine", "a")
		if want, got := modeEmpty, s.Mode(); want != got {
			t.Errorf("mode: want %v, got %v", want, got)
		}
		if want, got := "0", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})

	t.Run("shows empty with no sections when no task remains", func(t *testing.T) {
		var s state
		s.Snapshot("x", nil)
		if want, got := modeEmpty, s.Mode(); want != got {
			t.Errorf("mode: want %v, got %v", want, got)
		}
		assertSections(t, &s)
	})

	t.Run("the sort control reorders the main list and not the sections", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{
			{Key: "m-old-due-first", Source: "x", Due: day(1), Created: day(1)},
			{Key: "m-new-due-last", Source: "x", Due: day(8), Created: day(8)},
			sec("x", "s-old", "Mine", 2),
			sec("x", "s-new", "Mine", 7),
		})
		assertKeys(t, &s, "m-old-due-first", "m-new-due-last")
		s.SetSort(sortAdded)
		assertKeys(t, &s, "m-new-due-last", "m-old-due-first")
		assertSections(t, &s, "## Mine", "s-new", "s-old")
	})
}

func TestStateDismiss(t *testing.T) {
	t.Run("hides a main-list task and lowers the count", func(t *testing.T) {
		var s state
		a, b := tk("x", "a", day(1)), tk("x", "b", day(2))
		s.Snapshot("x", []task.Task{a, b})
		if err := s.Dismiss(a); err != nil {
			t.Fatal(err)
		}
		assertKeys(t, &s, "b")
		if want, got := "1", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})

	t.Run("hides a section task and drops its heading once the section is empty", func(t *testing.T) {
		var s state
		a := sec("x", "a", "Mine", 1)
		s.Snapshot("x", []task.Task{{Key: "m", Source: "x"}, a})
		s.Dismiss(a)
		assertKeys(t, &s, "m")
		assertSections(t, &s)
	})

	t.Run("dismissing the last task shows empty", func(t *testing.T) {
		var s state
		a := tk("x", "a", day(1))
		s.Snapshot("x", []task.Task{a})
		s.Dismiss(a)
		if want, got := modeEmpty, s.Mode(); want != got {
			t.Errorf("mode: want %v, got %v", want, got)
		}
	})

	t.Run("a task shows again when its source reports a different created time", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{tk("x", "a", day(1))})
		s.Dismiss(tk("x", "a", day(1)))
		s.Snapshot("x", []task.Task{tk("x", "a", day(2))})
		assertKeys(t, &s, "a")
	})

	t.Run("the same key in another source stays visible", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{tk("x", "a", day(1))})
		s.Snapshot("y", []task.Task{tk("y", "a", day(1))})
		s.Dismiss(tk("x", "a", day(1)))
		if want, got := []string{"a"}, keys(s.tasks); !slices.Equal(want, got) || s.tasks[0].Source != "y" {
			t.Errorf("tasks: want %v from y, got %v", want, s.tasks)
		}
	})
}

func TestStateDismissPruning(t *testing.T) {
	t.Run("a source's snapshot drops its dismissals that no longer match", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{tk("x", "a", day(1)), tk("x", "b", day(1))})
		s.Dismiss(tk("x", "a", day(1)))
		s.Dismiss(tk("x", "b", day(1)))
		s.Snapshot("x", []task.Task{tk("x", "a", day(1))})
		if want, got := []string{"a"}, entryKeys(s.dismissed); !slices.Equal(want, got) {
			t.Errorf("entries: want %v, got %v", want, got)
		}
	})

	t.Run("another source's snapshot leaves them alone", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{tk("x", "a", day(1))})
		s.Dismiss(tk("x", "a", day(1)))
		s.Snapshot("y", nil)
		if want, got := []string{"a"}, entryKeys(s.dismissed); !slices.Equal(want, got) {
			t.Errorf("entries: want %v, got %v", want, got)
		}
	})

	t.Run("dismissals loaded at start survive until their source delivers", func(t *testing.T) {
		var s state
		s.dismissed.Add(tk("x", "a", day(1)))
		s.Snapshot("y", nil)
		s.SignedOut("x")
		if want, got := 1, len(s.dismissed.entries); want != got {
			t.Errorf("entries: want %d, got %d", want, got)
		}
	})

	t.Run("a snapshot with untitled tasks still prunes", func(t *testing.T) {
		var s state
		s.dismissed.Add(tk("x", "gone", day(1)))
		s.Snapshot("x", []task.Task{{Key: "new", Source: "x", Title: ""}})
		if want, got := 0, len(s.dismissed.entries); want != got {
			t.Errorf("entries: want %d, got %d", want, got)
		}
	})

	t.Run("dismissals loaded from the file hide the task, and pruning rewrites it", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "d.json")
		var first state
		first.dismissedPath = path
		first.Snapshot("x", []task.Task{tk("x", "a", day(1))})
		if err := first.Dismiss(tk("x", "a", day(1))); err != nil {
			t.Fatal(err)
		}

		var second state
		second.dismissedPath = path
		second.dismissed = loadDismissals(path)
		second.Snapshot("x", []task.Task{tk("x", "a", day(1)), tk("x", "b", day(1))})
		assertKeys(t, &second, "b")

		if err := second.Snapshot("x", nil); err != nil {
			t.Fatal(err)
		}
		if got := loadDismissals(path); len(got.entries) != 0 {
			t.Errorf("want file pruned, got %v", got.entries)
		}
	})

	t.Run("a save failure is returned", func(t *testing.T) {
		var s state
		s.dismissedPath = filepath.Join(t.TempDir(), "file", "d.json")
		if err := writeBlocker(filepath.Dir(s.dismissedPath)); err != nil {
			t.Fatal(err)
		}
		s.Snapshot("x", []task.Task{tk("x", "a", day(1))})
		if err := s.Dismiss(tk("x", "a", day(1))); err == nil {
			t.Error("want error")
		}
	})
}
