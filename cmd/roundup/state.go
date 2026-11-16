package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

type sortMode int

const (
	sortDue sortMode = iota
	sortAdded
)

type sourceState struct {
	info      task.Info
	tasks     []task.Task
	loaded    bool
	signedOut bool
	signingIn bool
	signInErr string
}

// state is the data the panel and tray show; it is only touched from the main thread.
type state struct {
	sources map[string]*sourceState
	sort    sortMode
	// tasks is the main list: visible tasks with no section, in sort order.
	tasks []task.Task
	// sections are the headed blocks below the main list, in display order.
	sections []section
	// dismissed is the set of hidden tasks, saved to dismissedPath when that is set.
	dismissed     dismissals
	dismissedPath string
	// noSources is set when no source is configured.
	noSources bool
}

// row is a section heading, when heading is set, or a task.
type row struct {
	heading string
	task    task.Task
}

// section is a heading and its visible tasks, newest first.
type section struct {
	heading string
	fixed   bool
	tasks   []task.Task
}

func (s *state) source(id string) *sourceState {
	if s.sources == nil {
		s.sources = map[string]*sourceState{}
	}
	if s.sources[id] == nil {
		s.sources[id] = &sourceState{}
	}
	return s.sources[id]
}

// AddSource records a source's description, which says how its sections are ordered and which icons its tasks use.
func (s *state) AddSource(info task.Info) {
	s.source(info.ID).info = info
	s.rebuild()
}

// sectionDefs is the declared sections by name; when sources declare the same name, the lowest source ID wins.
func (s *state) sectionDefs() map[string]task.Section {
	defs := map[string]task.Section{}
	for _, id := range slices.Sorted(maps.Keys(s.sources)) {
		for _, def := range s.sources[id].info.Sections {
			if _, ok := defs[def.Name]; !ok {
				defs[def.Name] = def
			}
		}
	}
	return defs
}

// Snapshot replaces a source's tasks and forgets its dismissals that no longer match one; the error is from saving them.
func (s *state) Snapshot(name string, tasks []task.Task) error {
	src := s.source(name)
	src.tasks = tasks
	src.loaded = true
	src.signedOut = false
	pruned := s.dismissed.Prune(name, tasks)
	s.rebuild()
	if pruned {
		return s.saveDismissed()
	}
	return nil
}

// Dismiss hides the task as it is now; the error is from saving the dismissal.
func (s *state) Dismiss(t task.Task) error {
	if !s.dismissed.Add(t) {
		return nil
	}
	s.rebuild()
	return s.saveDismissed()
}

func (s *state) saveDismissed() error {
	if s.dismissedPath == "" {
		return nil
	}
	return s.dismissed.save(s.dismissedPath)
}

// SignedOut forgets the source's tasks and notes that it needs the person to sign in.
func (s *state) SignedOut(name string) {
	src := s.source(name)
	src.tasks = nil
	src.loaded = false
	src.signedOut = true
	s.rebuild()
}

func (s *state) SignInStarted(name string) {
	src := s.source(name)
	src.signingIn = true
	src.signInErr = ""
}

func (s *state) SignInFailed(name string, err error) {
	src := s.source(name)
	src.signingIn = false
	src.signInErr = err.Error()
}

func (s *state) SignedIn(name string) {
	src := s.source(name)
	src.signingIn = false
	src.signInErr = ""
	src.signedOut = false
}

func (s *state) SetSort(m sortMode) {
	s.sort = m
	s.rebuild()
}

// SignedOutIDs lists the signed-out sources by ID.
func (s *state) SignedOutIDs() []string {
	var ids []string
	for id, src := range s.sources {
		if src.signedOut {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

func (s *state) loadedAny() bool {
	for _, src := range s.sources {
		if src.loaded {
			return true
		}
	}
	return false
}

func (s *state) rebuild() {
	s.tasks = nil
	byName := map[string][]task.Task{}
	for _, src := range s.sources {
		for _, t := range src.tasks {
			switch {
			case s.dismissed.Has(t):
			case t.Section == "":
				s.tasks = append(s.tasks, t)
			default:
				byName[t.Section] = append(byName[t.Section], t)
			}
		}
	}
	slices.SortFunc(s.tasks, s.sort.compare)

	defs := s.sectionDefs()
	s.sections = nil
	names := slices.SortedFunc(maps.Keys(byName), func(a, b string) int {
		return cmp.Or(cmp.Compare(defs[a].Order, defs[b].Order), cmp.Compare(a, b))
	})
	for _, name := range names {
		tasks := byName[name]
		slices.SortFunc(tasks, compareNewest)
		s.sections = append(s.sections, section{heading: name, fixed: defs[name].Fixed, tasks: tasks})
	}
}

// scrollingRows flattens the sections that share the panel's space with the main list into list rows.
func (s *state) scrollingRows() []row { return s.rowsFor(false) }

// fixedRows flattens the sections that always show in full into list rows.
func (s *state) fixedRows() []row { return s.rowsFor(true) }

func (s *state) rowsFor(fixed bool) []row {
	var rows []row
	for _, sec := range s.sections {
		if sec.fixed != fixed {
			continue
		}
		rows = append(rows, row{heading: sec.heading})
		for _, t := range sec.tasks {
			rows = append(rows, row{task: t})
		}
	}
	return rows
}

func compareNewest(a, b task.Task) int {
	return cmp.Or(b.Created.Compare(a.Created), cmp.Compare(a.Key, b.Key), cmp.Compare(a.Source, b.Source))
}

func (m sortMode) compare(a, b task.Task) int {
	var c int
	if m == sortAdded {
		c = b.Created.Compare(a.Created)
	} else {
		c = compareDue(a, b)
	}
	return cmp.Or(c, cmp.Compare(a.Key, b.Key), cmp.Compare(a.Source, b.Source))
}

func compareDue(a, b task.Task) int {
	switch {
	case a.Due.IsZero() && b.Due.IsZero():
		return a.Created.Compare(b.Created)
	case a.Due.IsZero():
		return 1
	case b.Due.IsZero():
		return -1
	}
	return cmp.Or(a.Due.Compare(b.Due), a.Created.Compare(b.Created))
}

type mode int

const (
	modeLoading mode = iota
	modeList
	modeEmpty
	modeNoSources
)

// Mode says what the main area shows; the sections area depends only on whether there are sections.
func (s *state) Mode() mode {
	switch {
	case s.noSources:
		return modeNoSources
	case !s.loadedAny():
		return modeLoading
	case len(s.tasks) == 0:
		return modeEmpty
	default:
		return modeList
	}
}

// readout is what the menu bar item shows beside its icon.
type readout struct {
	// total is the open-task count, empty before the first snapshot.
	total string
	// failed counts the tasks whose outcome is failed.
	failed int
	// succeeded counts the tasks whose outcome is succeeded.
	succeeded int
}

func (s *state) Readout() readout {
	r := readout{total: s.TrayTitle()}
	count := func(t task.Task) {
		switch t.Outcome {
		case task.OutcomeFailed:
			r.failed++
		case task.OutcomeSucceeded:
			r.succeeded++
		}
	}
	for _, t := range s.tasks {
		count(t)
	}
	for _, sec := range s.sections {
		for _, t := range sec.tasks {
			count(t)
		}
	}
	return r
}

// TrayTitle is the open-task count, or nothing before the first snapshot.
func (s *state) TrayTitle() string {
	if !s.loadedAny() {
		return ""
	}
	return strconv.Itoa(len(s.tasks))
}

const loadingTitle = "Loading…"

// rowTitle is the text to show for a task's title, and whether it is a placeholder.
func rowTitle(t task.Task) (text string, placeholder bool) {
	if t.Title == "" {
		return loadingTitle, true
	}
	return t.Title, false
}

func dueLabel(due, now time.Time) string {
	if due.IsZero() {
		return ""
	}
	due = due.In(now.Location())
	if due.Year() == now.Year() {
		return fmt.Sprintf("due %d %s", due.Day(), due.Format("Jan"))
	}
	return fmt.Sprintf("due %d %s %d", due.Day(), due.Format("Jan"), due.Year())
}

// formatClock renders a duration as m:ss, or h:mm:ss from an hour.
func formatClock(d time.Duration) string {
	secs := int(max(d, 0) / time.Second)
	h, m, s := secs/3600, secs/60%60, secs%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
}

// readoutText is a readout's text, with a live clock drawn from its Since.
func readoutText(r task.Readout, now time.Time) string {
	switch r.Clock {
	case task.ClockElapsed:
		return formatClock(now.Sub(r.Since))
	case task.ClockAgo:
		return formatAgo(now.Sub(r.Since))
	}
	return r.Text
}
