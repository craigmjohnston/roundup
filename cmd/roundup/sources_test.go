package main

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/config"
	"github.com/craigmjohnston/roundup/task"
)

func TestOpenSources(t *testing.T) {
	commands := func(names ...string) []config.Source {
		var out []config.Source
		for _, name := range names {
			out = append(out, config.Source{Command: []string{name}})
		}
		return out
	}
	ids := func(sources []task.Source) []string {
		var out []string
		for _, src := range sources {
			out = append(out, src.Info().ID)
		}
		return out
	}

	t.Run("opens every program at once and keeps the configured order", func(t *testing.T) {
		delays := map[string]time.Duration{"slow": 300 * time.Millisecond, "medium": 200 * time.Millisecond, "quick": 0}
		var mu sync.Mutex
		var finished []string
		open := func(src config.Source) (task.Source, error) {
			command := src.Command
			time.Sleep(delays[command[0]])
			mu.Lock()
			defer mu.Unlock()
			finished = append(finished, command[0])
			return infoSource{task.Info{ID: command[0]}}, nil
		}

		start := time.Now()
		sources := openSources(commands("slow", "medium", "quick"), open)
		elapsed := time.Since(start)

		if want := []string{"slow", "medium", "quick"}; !slices.Equal(want, ids(sources)) {
			t.Errorf("want order %v, got %v", want, ids(sources))
		}
		if want := []string{"quick", "medium", "slow"}; !slices.Equal(want, finished) {
			t.Errorf("want them to finish as %v, got %v", want, finished)
		}
		if elapsed >= 500*time.Millisecond {
			t.Errorf("want startup to take about the slowest program (300ms), got %s", elapsed)
		}
	})

	t.Run("leaves out a program that fails to start", func(t *testing.T) {
		open := func(src config.Source) (task.Source, error) {
			command := src.Command
			if command[0] == "broken" {
				return nil, errors.New("no hello")
			}
			return infoSource{task.Info{ID: command[0]}}, nil
		}
		sources := openSources(commands("first", "broken", "last"), open)
		if want := []string{"first", "last"}; !slices.Equal(want, ids(sources)) {
			t.Errorf("want %v, got %v", want, ids(sources))
		}
	})

	t.Run("with nothing configured opens nothing", func(t *testing.T) {
		open := func(config.Source) (task.Source, error) { return nil, errors.New("unexpected") }
		if got := openSources(nil, open); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
}

func TestBuildSourcesSkipsPackageSourcesWithoutGo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	cfg := config.Config{Sources: []config.Source{{Package: "example.com/acme/tool@latest", Args: []string{"-x"}}}}

	if got := buildSources(cfg); len(got) != 0 {
		t.Errorf("want no sources, got %v", got)
	}
}
