package main

import "github.com/craigmjohnston/roundup/task"

func refreshSources(sources []task.Source) {
	for _, src := range sources {
		if r, ok := src.(task.Refresher); ok {
			r.Refresh()
		}
	}
}
