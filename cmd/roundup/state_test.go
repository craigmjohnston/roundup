package main

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func day(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.UTC) }

func keys(tasks []task.Task) []string {
	var ks []string
	for _, t := range tasks {
		ks = append(ks, t.Key)
	}
	return ks
}

func assertKeys(t *testing.T, s *state, want ...string) {
	t.Helper()
	if got := keys(s.tasks); !slices.Equal(want, got) {
		t.Errorf("order: want %v, got %v", want, got)
	}
}

func TestStateModes(t *testing.T) {
	two := []task.Task{{Key: "a", Source: "x"}, {Key: "b", Source: "x"}}

	t.Run("shows an empty body with no number before the first snapshot", func(t *testing.T) {
		var s state
		if want, got := modeLoading, s.Mode(); want != got {
			t.Errorf("mode: want %v, got %v", want, got)
		}
		if want, got := "", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})

	t.Run("shows the list and its count after a snapshot", func(t *testing.T) {
		var s state
		s.Snapshot("x", two)
		if want, got := modeList, s.Mode(); want != got {
			t.Errorf("mode: want %v, got %v", want, got)
		}
		if want, got := "2", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})

	t.Run("shows empty when a snapshot has no tasks", func(t *testing.T) {
		var s state
		s.Snapshot("x", nil)
		if want, got := modeEmpty, s.Mode(); want != got {
			t.Errorf("mode: want %v, got %v", want, got)
		}
		if want, got := "0", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})
}

func TestStateMerge(t *testing.T) {
	t.Run("merges the snapshots of two sources", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{{Key: "a", Source: "x", Due: day(3)}})
		s.Snapshot("y", []task.Task{{Key: "b", Source: "y", Due: day(1)}})
		assertKeys(t, &s, "b", "a")
		if want, got := "2", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})

	t.Run("replacing one source's snapshot leaves the other's", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{{Key: "a", Source: "x", Due: day(3)}})
		s.Snapshot("y", []task.Task{{Key: "b", Source: "y", Due: day(1)}})
		s.Snapshot("x", []task.Task{{Key: "c", Source: "x", Due: day(2)}})
		assertKeys(t, &s, "b", "c")
	})

	t.Run("counts a source's tasks once another source has delivered", func(t *testing.T) {
		var s state
		s.Snapshot("y", nil)
		if want, got := "0", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})
}

func TestStateMerge_AcrossSources(t *testing.T) {
	t.Run("date added interleaves tasks from different sources newest first", func(t *testing.T) {
		var s state
		s.SetSort(sortAdded)
		s.Snapshot("source-a", []task.Task{{Key: "s1", Source: "source-a", Created: day(1)}, {Key: "s2", Source: "source-a", Created: day(5)}})
		s.Snapshot("source-b", []task.Task{{Key: "g1", Source: "source-b", Created: day(3)}, {Key: "g2", Source: "source-b", Created: day(7)}})
		assertKeys(t, &s, "g2", "s2", "g1", "s1")
		if want, got := "4", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})

	t.Run("due date puts dated tasks before undated ones from another source", func(t *testing.T) {
		var s state
		s.Snapshot("source-a", []task.Task{{Key: "s1", Source: "source-a", Due: day(9), Created: day(1)}})
		s.Snapshot("source-b", []task.Task{{Key: "g1", Source: "source-b", Created: day(3)}, {Key: "g2", Source: "source-b", Created: day(2)}})
		assertKeys(t, &s, "s1", "g2", "g1")
	})
}

func TestStateSort(t *testing.T) {
	t.Run("due date puts earliest first, undated last, ties by created then key", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{
			{Key: "undated-new", Created: day(9)},
			{Key: "late", Due: day(5), Created: day(1)},
			{Key: "tie-b", Due: day(2), Created: day(4)},
			{Key: "tie-a", Due: day(2), Created: day(4)},
			{Key: "tie-old", Due: day(2), Created: day(3)},
			{Key: "undated-old", Created: day(8)},
		})
		assertKeys(t, &s, "tie-old", "tie-a", "tie-b", "late", "undated-old", "undated-new")
	})

	t.Run("date added puts newest first, ties by key", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{
			{Key: "old", Created: day(1), Due: day(1)},
			{Key: "new-b", Created: day(7)},
			{Key: "new-a", Created: day(7)},
			{Key: "mid", Created: day(4)},
		})
		s.SetSort(sortAdded)
		assertKeys(t, &s, "new-a", "new-b", "mid", "old")
	})

	t.Run("orders the same whichever source delivers first", func(t *testing.T) {
		x := []task.Task{{Key: "k", Source: "x", Due: day(2)}}
		y := []task.Task{{Key: "k", Source: "y", Due: day(2)}}
		var s1, s2 state
		s1.Snapshot("x", x)
		s1.Snapshot("y", y)
		s2.Snapshot("y", y)
		s2.Snapshot("x", x)
		if want, got := s1.tasks[0].Source, s2.tasks[0].Source; want != got {
			t.Errorf("first source: want %q, got %q", want, got)
		}
	})

	t.Run("switching back restores due date order", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{{Key: "a", Due: day(2), Created: day(1)}, {Key: "b", Due: day(1), Created: day(2)}})
		s.SetSort(sortAdded)
		assertKeys(t, &s, "b", "a")
		s.SetSort(sortDue)
		assertKeys(t, &s, "b", "a")
		s.Snapshot("x", []task.Task{{Key: "a", Due: day(1), Created: day(1)}, {Key: "b", Due: day(2), Created: day(2)}})
		assertKeys(t, &s, "a", "b")
	})
}

func TestStateSignedOut(t *testing.T) {
	t.Run("removes only that source's tasks and flags it", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{{Key: "a", Source: "x"}})
		s.Snapshot("y", []task.Task{{Key: "b", Source: "y"}})
		s.SignedOut("x")
		assertKeys(t, &s, "b")
		if want, got := []string{"x"}, s.SignedOutIDs(); !slices.Equal(want, got) {
			t.Errorf("signed out: want %v, got %v", want, got)
		}
		if want, got := "1", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
	})

	t.Run("drops the count when the only source is signed out", func(t *testing.T) {
		var s state
		s.Snapshot("x", []task.Task{{Key: "a"}})
		s.SignedOut("x")
		if want, got := "", s.TrayTitle(); want != got {
			t.Errorf("title: want %q, got %q", want, got)
		}
		if want, got := modeLoading, s.Mode(); want != got {
			t.Errorf("mode: want %v, got %v", want, got)
		}
	})

	t.Run("a new snapshot clears the flag", func(t *testing.T) {
		var s state
		s.SignedOut("x")
		s.Snapshot("x", nil)
		if got := s.SignedOutIDs(); len(got) != 0 {
			t.Errorf("signed out: want none, got %v", got)
		}
	})
}

func TestStateSignIn(t *testing.T) {
	t.Run("records the failure text and allows another attempt", func(t *testing.T) {
		var s state
		s.SignedOut("x")
		s.SignInStarted("x")
		if !s.source("x").signingIn {
			t.Error("want signing in")
		}
		s.SignInFailed("x", errors.New("chrome closed"))
		if s.source("x").signingIn {
			t.Error("want not signing in")
		}
		if want, got := "chrome closed", s.source("x").signInErr; want != got {
			t.Errorf("error: want %q, got %q", want, got)
		}
		if want, got := []string{"x"}, s.SignedOutIDs(); !slices.Equal(want, got) {
			t.Errorf("signed out: want %v, got %v", want, got)
		}
	})

	t.Run("clears the failure text on a new attempt and on success", func(t *testing.T) {
		var s state
		s.SignInFailed("x", errors.New("boom"))
		s.SignInStarted("x")
		if want, got := "", s.source("x").signInErr; want != got {
			t.Errorf("after start: want %q, got %q", want, got)
		}
		s.SignInFailed("x", errors.New("boom"))
		s.SignedIn("x")
		if want, got := "", s.source("x").signInErr; want != got {
			t.Errorf("after success: want %q, got %q", want, got)
		}
	})

	t.Run("signing in clears the signed-out flag", func(t *testing.T) {
		var s state
		s.SignedOut("x")
		s.SignInStarted("x")
		s.SignedIn("x")
		if got := s.SignedOutIDs(); len(got) != 0 {
			t.Errorf("signed out: want none, got %v", got)
		}
	})

	t.Run("one source signing in leaves another untouched", func(t *testing.T) {
		var s state
		s.SignInStarted("x")
		if s.source("y").signingIn {
			t.Error("y should not be signing in")
		}
	})
}

func TestDueLabel(t *testing.T) {
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		due  time.Time
		want string
	}{
		{"this year omits the year", time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), "due 3 Nov"},
		{"other year includes the year", time.Date(2024, 11, 3, 12, 0, 0, 0, time.UTC), "due 3 Nov 2024"},
		{"next year includes the year", time.Date(2027, 1, 20, 12, 0, 0, 0, time.UTC), "due 20 Jan 2027"},
		{"no due date is empty", time.Time{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := dueLabel(tc.due, now); tc.want != got {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestRowTitle(t *testing.T) {
	t.Run("shows a muted Loading placeholder while the title is empty", func(t *testing.T) {
		text, placeholder := rowTitle(task.Task{Detail: "#devs"})

		if want := "Loading…"; want != text || !placeholder {
			t.Errorf("want %q placeholder, got %q placeholder=%v", want, text, placeholder)
		}
	})

	t.Run("shows the title once it is known", func(t *testing.T) {
		text, placeholder := rowTitle(task.Task{Title: "do it"})

		if want := "do it"; want != text || placeholder {
			t.Errorf("want %q, got %q placeholder=%v", want, text, placeholder)
		}
	})
}
