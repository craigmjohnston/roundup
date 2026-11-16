package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/craigmjohnston/roundup/plugins/slack"
	"github.com/craigmjohnston/roundup/task"
)

const titleWidth = 80

func tasksCmd(resolve func() (slack.Workspace, error)) *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "List the open Slack Later tasks, and with --watch print each change",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolve()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return runTasks(ctx, slack.NewLater(ws), watch, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "keep running and print each change to the list")
	return cmd
}

func runTasks(ctx context.Context, later *slack.Later, watch bool, out, errOut io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	later.OnDisconnect = func(err error) {
		fmt.Fprintf(errOut, "connection lost, reconnecting: %v\n", err)
	}

	p := &printer{out: out}
	later.OnLoaded = func() {
		first := !p.printed
		p.loaded()
		if first && !watch {
			cancel()
		}
	}
	return later.Run(ctx, p.update)
}

// printer prints the list once every task has its text, then each change after that.
type printer struct {
	out     io.Writer
	latest  []task.Task
	prev    []task.Task
	printed bool
}

func (p *printer) update(tasks []task.Task) {
	p.latest = tasks
	if p.printed && allTitled(tasks) {
		p.loaded()
	}
}

func (p *printer) loaded() {
	if !p.printed {
		p.printed = true
		for _, t := range p.latest {
			fmt.Fprintln(p.out, taskLine(t))
		}
		fmt.Fprintf(p.out, "%d tasks\n", len(p.latest))
	} else {
		printChanges(p.out, p.prev, p.latest)
	}
	p.prev = p.latest
}

func allTitled(tasks []task.Task) bool {
	for _, t := range tasks {
		if t.Title == "" {
			return false
		}
	}
	return true
}

func printChanges(out io.Writer, before, after []task.Task) {
	changes := diffTasks(before, after)
	if len(changes) == 0 {
		return
	}
	for _, c := range changes {
		fmt.Fprintln(out, c)
	}
	fmt.Fprintf(out, "%d tasks\n", len(after))
}

// diffTasks describes what changed between two snapshots, one line per task.
func diffTasks(before, after []task.Task) []string {
	old := make(map[string]task.Task, len(before))
	for _, t := range before {
		old[t.Key] = t
	}
	var lines []string
	seen := make(map[string]bool, len(after))
	for _, t := range after {
		seen[t.Key] = true
		prev, ok := old[t.Key]
		switch {
		case !ok:
			lines = append(lines, "+ added "+taskLine(t))
		case !sameTask(prev, t):
			lines = append(lines, "~ updated "+taskLine(t))
		}
	}
	for _, t := range before {
		if !seen[t.Key] {
			lines = append(lines, "- removed "+taskLine(t))
		}
	}
	return lines
}

func sameTask(a, b task.Task) bool {
	return a.Key == b.Key && a.Title == b.Title && a.Detail == b.Detail && a.URL == b.URL && a.Due.Equal(b.Due) && a.Created.Equal(b.Created)
}

func taskLine(t task.Task) string {
	due := "no due date"
	if !t.Due.IsZero() {
		due = t.Due.Local().Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%s | %s | %s", due, t.Detail, truncate(t.Title, titleWidth))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
