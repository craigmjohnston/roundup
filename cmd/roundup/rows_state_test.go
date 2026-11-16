package main

import (
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func TestSectionsAreOrderedByOrderThenNameWithFixedSectionsLast(t *testing.T) {
	var s state
	s.AddSource(task.Info{ID: "x", Sections: []task.Section{
		{Name: "Late", Order: 2},
		{Name: "Pinned", Order: 1, Fixed: true},
		{Name: "Early", Order: -1},
	}})
	s.Snapshot("x", []task.Task{
		sec("x", "z", "Zeta", 1),
		sec("x", "p", "Pinned", 2),
		sec("x", "l", "Late", 3),
		sec("x", "a", "Alpha", 4),
		sec("x", "e", "Early", 5),
	})
	assertSections(t, &s, "## Early", "e", "## Alpha", "a", "## Zeta", "z", "## Late", "l", "## Pinned", "p")
}

func TestASectionDeclaredByTwoSourcesTakesTheLowestSourceIDsDefinition(t *testing.T) {
	var s state
	s.AddSource(task.Info{ID: "b", Sections: []task.Section{{Name: "Shared", Order: 5}}})
	s.AddSource(task.Info{ID: "a", Sections: []task.Section{{Name: "Shared", Fixed: true}}})
	s.Snapshot("a", []task.Task{sec("a", "k", "Shared", 1)})
	if got := s.fixedRows(); len(got) != 2 {
		t.Errorf("fixed rows: want the section's heading and task, got %v", got)
	}
	if got := s.scrollingRows(); len(got) != 0 {
		t.Errorf("scrolling rows: want none, got %v", got)
	}
}

func TestFormatClock(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{59 * time.Second, "0:59"},
		{time.Minute, "1:00"},
		{59*time.Minute + 59*time.Second, "59:59"},
		{time.Hour, "1:00:00"},
		{2*time.Hour + 5*time.Minute + 9*time.Second, "2:05:09"},
		{-3 * time.Second, "0:00"},
	}
	for _, tc := range tests {
		if got := formatClock(tc.d); tc.want != got {
			t.Errorf("formatClock(%v): want %q, got %q", tc.d, tc.want, got)
		}
	}
}

func TestFormatAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "now"},
		{59 * time.Second, "now"},
		{time.Minute, "1m ago"},
		{4 * time.Minute, "4m ago"},
		{59*time.Minute + 59*time.Second, "59m ago"},
		{time.Hour, "1h ago"},
		{23*time.Hour + 59*time.Minute, "23h ago"},
		{24 * time.Hour, "1d ago"},
		{3 * 24 * time.Hour, "3d ago"},
	}
	for _, tc := range tests {
		if got := formatAgo(tc.d); tc.want != got {
			t.Errorf("formatAgo(%v): want %q, got %q", tc.d, tc.want, got)
		}
	}
}

func TestReadoutText(t *testing.T) {
	now := day(12)
	since := now.Add(-72 * time.Second)

	t.Run("an elapsed clock counts up from Since", func(t *testing.T) {
		if want, got := "1:12", readoutText(task.Readout{Since: since, Clock: task.ClockElapsed}, now); want != got {
			t.Errorf("want %q, got %q", want, got)
		}
	})
	t.Run("an ago clock shows how long since Since", func(t *testing.T) {
		if want, got := "4m ago", readoutText(task.Readout{Since: now.Add(-4 * time.Minute), Clock: task.ClockAgo}, now); want != got {
			t.Errorf("want %q, got %q", want, got)
		}
	})
	t.Run("a readout with no clock shows its text", func(t *testing.T) {
		if want, got := "6/7 jobs", readoutText(task.Readout{Text: "6/7 jobs", Since: since}, now); want != got {
			t.Errorf("want %q, got %q", want, got)
		}
	})
}
