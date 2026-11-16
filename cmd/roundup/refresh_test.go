package main

import (
	"context"
	"testing"

	"github.com/craigmjohnston/roundup/task"
)

type pushSource struct{}

func (pushSource) Info() task.Info                              { return task.Info{ID: "push"} }
func (pushSource) Run(context.Context, func([]task.Task)) error { return nil }

type pollSource struct {
	pushSource
	refreshes int
}

func (p *pollSource) Refresh() { p.refreshes++ }

func TestRefreshSources(t *testing.T) {
	t.Run("refreshes every source that can be refreshed once", func(t *testing.T) {
		a, b := &pollSource{}, &pollSource{}
		refreshSources([]task.Source{a, b})
		if a.refreshes != 1 || b.refreshes != 1 {
			t.Errorf("want 1 and 1, got %d and %d", a.refreshes, b.refreshes)
		}
	})

	t.Run("skips sources that are not refreshable", func(t *testing.T) {
		a := &pollSource{}
		refreshSources([]task.Source{pushSource{}, a, pushSource{}})
		if a.refreshes != 1 {
			t.Errorf("want 1, got %d", a.refreshes)
		}
	})
}
