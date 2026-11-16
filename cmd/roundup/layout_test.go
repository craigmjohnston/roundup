package main

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

func TestShareHeights(t *testing.T) {
	tests := []struct {
		name         string
		avail, a, b  float32
		wantA, wantB float32
	}{
		{name: "both fit and keep what they need", avail: 500, a: 100, b: 250, wantA: 100, wantB: 250},
		{name: "a short first list is shown in full and the second takes the rest", avail: 500, a: 100, b: 900, wantA: 100, wantB: 400},
		{name: "a short second list is shown in full and the first takes the rest", avail: 500, a: 900, b: 120, wantA: 380, wantB: 120},
		{name: "two long lists get half each", avail: 500, a: 900, b: 900, wantA: 250, wantB: 250},
		{name: "a list just over half is cut to half when the other is long", avail: 500, a: 260, b: 900, wantA: 250, wantB: 250},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotA, gotB := shareHeights(tc.avail, tc.a, tc.b)
			if gotA != tc.wantA || gotB != tc.wantB {
				t.Errorf("want %v and %v, got %v and %v", tc.wantA, tc.wantB, gotA, gotB)
			}
		})
	}
}

func layoutState(mainTasks, sharedRows, fixedRows int) *state {
	s := &state{}
	s.AddSource(testInfo)
	tasks := manyItems("main", mainTasks, "")
	tasks = append(tasks, manyItems("shared", sharedRows, sectionFirst)...)
	tasks = append(tasks, manyItems("fixed", fixedRows, sectionFixed)...)
	s.Snapshot("test", tasks)
	return s
}

func TestLayout(t *testing.T) {
	t.Run("a short panel is only as tall as its content", func(t *testing.T) {
		l := layoutState(2, 5, 1).layout()

		assertNear(t, "main", 2*rowHeight+listPadding, l.main)
		assertNear(t, "sections", headingHeight+5*rowHeight+sectionsPadding, l.sections)
		assertNear(t, "fixed", headingHeight+rowHeight+sectionsPadding, l.fixed)
		if l.total >= maxPanelHeight {
			t.Errorf("want a total under %d, got %v", maxPanelHeight, l.total)
		}
		assertNear(t, "total", float32(math.Ceil(float64(headerHeight+l.main+l.sections+l.fixed+2*dividerHeight))), l.total)
	})

	t.Run("fixed sections are never cut, and long lists split the rest evenly", func(t *testing.T) {
		l := layoutState(20, 20, 3).layout()

		assertNear(t, "fixed", headingHeight+3*rowHeight+sectionsPadding, l.fixed)
		assertNear(t, "main and sections", l.main, l.sections)
		assertNear(t, "total", maxPanelHeight, l.total)
	})

	t.Run("two tasks show in full beside many section rows", func(t *testing.T) {
		l := layoutState(2, 20, 1).layout()

		assertNear(t, "main", 2*rowHeight+listPadding, l.main)
		assertNear(t, "total", maxPanelHeight, l.total)
	})

	t.Run("an empty state with nothing else has a body", func(t *testing.T) {
		l := layoutState(0, 0, 0).layout()

		assertNear(t, "total", float32(math.Ceil(headerHeight+emptyBodyHeight)), l.total)
	})

	t.Run("before any source answers the panel is short", func(t *testing.T) {
		l := (&state{}).layout()

		assertNear(t, "total", float32(math.Ceil(headerHeight+loadingHeight)), l.total)
	})
}

func TestLayoutMatchesTheRenderedPanel(t *testing.T) {
	a := &app{}
	a.useDismissals(filepath.Join(t.TempDir(), "dismissed.json"))
	a.st.AddSource(testInfo)
	tasks := manyItems("main", 2, "")
	tasks = append(tasks, manyItems("shared", 3, sectionFirst)...)
	tasks = append(tasks, task.Task{Source: "test", Key: "fixed", Title: "the fixed row", Detail: "fixed detail", Section: sectionFixed, Created: day(1)})
	a.st.Snapshot("test", tasks)
	l := a.st.layout()
	tt := ui.NewTesterWith(a.view, flyoutWidth, int(l.total), ui.TesterOptions{})
	tt.Frame()

	heading, ok := tt.Find(sectionFixed)
	if !ok {
		t.Fatalf("fixed heading not found; texts: %q", tt.Texts())
	}
	assertNear(t, "fixed heading top", headerHeight+l.main+dividerHeight+l.sections+dividerHeight, heading.Y)

	detail, ok := tt.Find("fixed detail")
	if !ok {
		t.Fatalf("fixed row not found; texts: %q", tt.Texts())
	}
	const rowBottomPadding = 7
	assertNear(t, "panel bottom", l.total, detail.Y+detail.H+rowBottomPadding+sectionsPadding)

	first, ok := tt.Find("main task 0")
	if !ok {
		t.Fatalf("first task not found; texts: %q", tt.Texts())
	}
	const rowTopPadding = 7
	assertNear(t, "first row top", headerHeight+listPadding/2+rowTopPadding, first.Y)
}

func assertNear(t *testing.T, what string, want, got float32) {
	t.Helper()
	if math.Abs(float64(want-got)) > 1.5 {
		t.Errorf("%s: want %v, got %v", what, want, got)
	}
}
