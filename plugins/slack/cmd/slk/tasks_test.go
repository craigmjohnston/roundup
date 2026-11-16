package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func TestDiffTasks(t *testing.T) {
	a := task.Task{Key: "a", Title: "alpha", Detail: "#devs · Jane"}
	b := task.Task{Key: "b", Title: "beta", Detail: "#devs · Bob"}

	t.Run("reports added, updated and removed tasks", func(t *testing.T) {
		a2 := a
		a2.Due = time.Date(2026, 11, 12, 9, 0, 0, 0, time.Local)
		c := task.Task{Key: "c", Title: "gamma", Detail: "#ops · Al"}

		got := diffTasks([]task.Task{a, b}, []task.Task{a2, c})

		want := []string{
			"~ updated " + taskLine(a2),
			"+ added " + taskLine(c),
			"- removed " + taskLine(b),
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("want %q, got %q", want, got)
		}
	})

	t.Run("reports nothing when the snapshots match", func(t *testing.T) {
		if got := diffTasks([]task.Task{a, b}, []task.Task{b, a}); len(got) != 0 {
			t.Fatalf("want no changes, got %q", got)
		}
	})
}

func TestPrintChanges(t *testing.T) {
	a := task.Task{Key: "a", Title: "alpha", Detail: "#devs"}

	t.Run("prints the changes and the new count", func(t *testing.T) {
		var out bytes.Buffer

		printChanges(&out, nil, []task.Task{a})

		assertEqual(t, "+ added no due date | #devs | alpha\n1 tasks\n", out.String())
	})

	t.Run("prints nothing when nothing changed", func(t *testing.T) {
		var out bytes.Buffer

		printChanges(&out, []task.Task{a}, []task.Task{a})

		assertEqual(t, "", out.String())
	})
}

func TestTaskLine(t *testing.T) {
	t.Run("shows the due date, detail and title", func(t *testing.T) {
		tk := task.Task{Title: "do it", Detail: "#devs · Jane", Due: time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)}

		assertEqual(t, "2026-11-12 09:30 | #devs · Jane | do it", taskLine(tk))
	})

	t.Run("cuts a long title", func(t *testing.T) {
		line := taskLine(task.Task{Title: strings.Repeat("é", 200), Detail: "d"})

		title := strings.TrimPrefix(line, "no due date | d | ")
		assertEqual(t, "80", itoa(len([]rune(title))))
		if !strings.HasSuffix(title, "…") {
			t.Errorf("want a trailing ellipsis, got %q", title)
		}
	})
}

func TestPrinter(t *testing.T) {
	loading := []task.Task{{Key: "a", Detail: "#devs"}, {Key: "b"}}
	done := []task.Task{{Key: "a", Title: "alpha", Detail: "#devs"}, {Key: "b", Title: "beta"}}

	t.Run("prints nothing until the lookups have finished, then the complete list", func(t *testing.T) {
		var out bytes.Buffer
		p := &printer{out: &out}

		p.update(loading)
		p.update(done)
		assertEqual(t, "", out.String())
		p.loaded()

		assertEqual(t, "no due date | #devs | alpha\nno due date |  | beta\n2 tasks\n", out.String())
	})

	t.Run("prints changes after the list, holding back tasks that have no title", func(t *testing.T) {
		var out bytes.Buffer
		p := &printer{out: &out}
		p.update(done)
		p.loaded()
		out.Reset()
		added := append([]task.Task{{Key: "c"}}, done...)

		p.update(added)
		assertEqual(t, "", out.String())
		added[0].Title = "gamma"
		p.loaded()

		assertEqual(t, "+ added no due date |  | gamma\n3 tasks\n", out.String())
	})
}
