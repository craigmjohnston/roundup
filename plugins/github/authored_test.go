package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

var testNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

type authoredAPI struct {
	srv *httptest.Server

	mu          sync.Mutex
	open        []authoredPR
	convos      map[string]prConversation
	status      int
	headers     map[string]string
	detailFails bool

	lists   []string
	details [][]string
	auths   []string
	times   []time.Time
}

func newAuthoredAPI(t *testing.T) *authoredAPI {
	t.Helper()
	f := &authoredAPI{convos: map[string]prConversation{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *authoredAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	f.times = append(f.times, time.Now())
	var body struct {
		Query     string `json:"query"`
		Variables struct {
			Open string   `json:"open"`
			IDs  []string `json:"ids"`
		} `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	rate := map[string]any{"cost": 1, "remaining": 4999}
	switch body.Query {
	case authoredListQuery:
		f.lists = append(f.lists, body.Variables.Open)
	case authoredDetailQuery:
		f.details = append(f.details, body.Variables.IDs)
	}
	for k, v := range f.headers {
		w.Header().Set(k, v)
	}
	if f.status != 0 || (body.Query == authoredDetailQuery && f.detailFails) {
		w.WriteHeader(cmpOr(f.status, http.StatusBadGateway))
		return
	}
	var data any
	switch body.Query {
	case authoredListQuery:
		var d authoredListData
		d.Viewer.Login = me
		d.Open.Nodes = f.open
		data = map[string]any{"viewer": d.Viewer, "open": d.Open, "rateLimit": rate}
	case authoredDetailQuery:
		var nodes []prConversation
		for _, id := range body.Variables.IDs {
			c := f.convos[id]
			c.ID = id
			nodes = append(nodes, c)
		}
		data = map[string]any{"nodes": nodes, "rateLimit": rate}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func cmpOr(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func (f *authoredAPI) with(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *authoredAPI) read(fn func()) { f.with(fn) }

func (f *authoredAPI) listCount() (n int) { f.read(func() { n = len(f.lists) }); return }

func (f *authoredAPI) detailCount() (n int) { f.read(func() { n = len(f.details) }); return }

func (f *authoredAPI) requestTimes() (out []time.Time) {
	f.read(func() { out = slices.Clone(f.times) })
	return
}

func openAt(number int, updated time.Time) authoredPR {
	pr := openPR(number)
	pr.UpdatedAt = updated
	return pr
}

func newAuthored(f *authoredAPI, interval time.Duration) *Authored {
	a := NewAuthored()
	a.baseURL, a.client, a.interval = f.srv.URL, f.srv.Client(), interval
	a.token = staticToken
	a.now = func() time.Time { return testNow }
	return a
}

type authoredRun struct {
	mu      sync.Mutex
	updates [][]task.Task
	done    chan error
	cancel  context.CancelFunc
	src     *Authored
}

func startAuthored(t *testing.T, src *Authored) *authoredRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &authoredRun{done: make(chan error, 1), cancel: cancel, src: src}
	go func() {
		r.done <- src.Run(ctx, func(tasks []task.Task) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.updates = append(r.updates, tasks)
		})
	}()
	t.Cleanup(cancel)
	return r
}

func (r *authoredRun) updateCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.updates)
}

func (r *authoredRun) stop(t *testing.T) [][]task.Task {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.updates)
}

func details(updates [][]task.Task) (out [][]string) {
	for _, u := range updates {
		var ds []string
		for _, tk := range u {
			ds = append(ds, tk.Detail)
		}
		out = append(out, ds)
	}
	return out
}

func equalStrings(t *testing.T, want, got []string) {
	t.Helper()
	if !slices.Equal(want, got) {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestAuthoredRun_Polling(t *testing.T) {
	t.Run("lists every poll and reads a conversation only when updatedAt changes", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() {
			f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour)), openAt(2, testNow.Add(-2*time.Hour))}
		})
		r := startAuthored(t, newAuthored(f, 5*time.Millisecond))
		waitFor(t, "three lists", func() bool { return f.listCount() >= 3 })
		if want, got := 1, f.detailCount(); want != got {
			t.Fatalf("detail reads: want %d, got %d", want, got)
		}
		f.read(func() { equalStrings(t, []string{"AP_01", "AP_02"}, f.details[0]) })

		f.with(func() { f.open[0].UpdatedAt = testNow.Add(-time.Minute) })
		waitFor(t, "the changed pull request's detail", func() bool { return f.detailCount() >= 2 })
		n := f.listCount()
		waitFor(t, "lists after the change", func() bool { return f.listCount() >= n+3 })
		r.stop(t)

		if want, got := 2, f.detailCount(); want != got {
			t.Errorf("detail reads: want %d, got %d", want, got)
		}
		f.read(func() { equalStrings(t, []string{"AP_01"}, f.details[1]) })
	})

	t.Run("sends the token and asks for open pull requests since the cutoff", func(t *testing.T) {
		f := newAuthoredAPI(t)
		r := startAuthored(t, newAuthored(f, time.Hour))
		waitFor(t, "a list", func() bool { return f.listCount() >= 1 })
		r.stop(t)

		f.read(func() {
			equalStrings(t, []string{"Bearer secret"}, f.auths[:1])
			assertEqual(t, "is:open is:pr author:@me archived:false updated:>=2026-10-29", f.lists[0])
		})
	})

	t.Run("publishes once for an unchanged set and again when the checks change", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() {
			f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour)).withRollup(rollup(map[string]int{"SUCCESS": 2, "PENDING": 1}, nil))}
		})
		r := startAuthored(t, newAuthored(f, 5*time.Millisecond))
		waitFor(t, "four lists", func() bool { return f.listCount() >= 4 })
		f.with(func() { f.open[0] = f.open[0].withRollup(rollup(map[string]int{"SUCCESS": 3}, nil)) })
		n := f.listCount()
		waitFor(t, "lists after the checks change", func() bool { return f.listCount() >= n+3 })
		updates := r.stop(t)

		assertEqual(t, 2, len(updates))
		assertEqual(t, task.Readout{Symbol: "circle.fill", Tone: task.ToneWarning, Text: "2/3"}, updates[0][0].Readouts[0])
		assertEqual(t, task.Readout{Symbol: "checkmark", Tone: task.ToneSuccess, Text: "3/3"}, updates[1][0].Readouts[0])
		assertEqual(t, 1, f.detailCount())
	})

	t.Run("counts unanswered comments from the conversation", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() {
			f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour))}
			f.convos["AP_01"] = convo([]conversationItem{item(person("sam"), t1)}, nil, lastComment(false, item(person("alex"), t1)))
		})
		r := startAuthored(t, newAuthored(f, time.Hour))
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })
		updates := r.stop(t)

		assertEqual(t, "2", updates[0][0].Readouts[0].Text)
		assertEqual(t, "Pull requests", updates[0][0].Section)
	})

	t.Run("drops pull requests with no activity for two weeks", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() {
			f.open = []authoredPR{openAt(1, testNow.Add(-15*24*time.Hour)), openAt(2, testNow.Add(-time.Hour))}
		})
		r := startAuthored(t, newAuthored(f, time.Hour))
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })
		updates := r.stop(t)

		equalStrings(t, []string{"widgets#2"}, details(updates)[0])
		f.read(func() { equalStrings(t, []string{"AP_02"}, f.details[0]) })
	})
}

func TestAuthoredRun_Failures(t *testing.T) {
	t.Run("keeps the last snapshot when a poll fails", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() { f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour))} })
		r := startAuthored(t, newAuthored(f, 5*time.Millisecond))
		waitFor(t, "the first update", func() bool { return r.updateCount() >= 1 })

		f.with(func() { f.status = http.StatusBadGateway })
		n := f.listCount()
		waitFor(t, "failing lists", func() bool { return f.listCount() >= n+3 })
		f.with(func() { f.status = 0; f.open = append(f.open, openAt(2, testNow.Add(-time.Hour))) })
		waitFor(t, "the recovery update", func() bool { return r.updateCount() >= 2 })
		updates := r.stop(t)

		assertEqual(t, 2, len(updates))
		equalStrings(t, []string{"widgets#1", "widgets#2"}, details(updates)[1])
	})

	t.Run("does not record a failed detail fetch as fetched", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() {
			f.detailFails = true
			f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour))}
			f.convos["AP_01"] = convo([]conversationItem{item(person("sam"), t1)}, nil)
		})
		r := startAuthored(t, newAuthored(f, 5*time.Millisecond))
		waitFor(t, "two failed detail reads", func() bool { return f.detailCount() >= 2 })
		assertEqual(t, 0, r.updateCount())

		f.with(func() { f.detailFails = false })
		waitFor(t, "the update after recovery", func() bool { return r.updateCount() >= 1 })
		updates := r.stop(t)

		assertEqual(t, "1", updates[0][0].Readouts[0].Text)
	})

	t.Run("ends with an error when github rejects the token", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() { f.status = http.StatusUnauthorized })
		r := startAuthored(t, newAuthored(f, 5*time.Millisecond))
		select {
		case err := <-r.done:
			if !errors.Is(err, errUnauthorized) {
				t.Errorf("want errUnauthorized, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not end")
		}
	})

	t.Run("ends with an error when there is no token", func(t *testing.T) {
		f := newAuthoredAPI(t)
		src := newAuthored(f, time.Hour)
		src.token = func(context.Context) (string, error) { return "", errNoToken }
		r := startAuthored(t, src)
		if err := <-r.done; !errors.Is(err, errNoToken) {
			t.Errorf("want errNoToken, got %v", err)
		}
	})

	t.Run("waits out a rate limit before polling again", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() { f.status, f.headers = http.StatusTooManyRequests, map[string]string{"Retry-After": "1"} })
		r := startAuthored(t, newAuthored(f, 5*time.Millisecond))
		waitFor(t, "the limited request", func() bool { return f.listCount() >= 1 })
		r.src.Refresh()
		f.with(func() { f.status, f.headers = 0, nil })
		waitFor(t, "the request after the wait", func() bool { return f.listCount() >= 2 })
		r.stop(t)

		times := f.requestTimes()
		if gap := times[1].Sub(times[0]); gap < 900*time.Millisecond {
			t.Errorf("polled again after %v", gap)
		}
	})
}

func TestAuthoredRefresh(t *testing.T) {
	t.Run("polls straight away instead of waiting for the interval", func(t *testing.T) {
		f := newAuthoredAPI(t)
		r := startAuthored(t, newAuthored(f, time.Hour))
		waitFor(t, "the first list", func() bool { return f.listCount() >= 1 })
		r.src.Refresh()
		waitFor(t, "the refreshed list", func() bool { return f.listCount() >= 2 })
		r.stop(t)
	})

	t.Run("never blocks and merges requests made before a poll", func(t *testing.T) {
		src := NewAuthored()
		for range 5 {
			src.Refresh()
		}
	})
}

func TestAuthoredRun_PullRequestFields(t *testing.T) {
	t.Run("fills the pull request status on every task", func(t *testing.T) {
		f := newAuthoredAPI(t)
		approved := openAt(1, testNow.Add(-time.Hour)).withRollup(rollup(map[string]int{"SUCCESS": 11, "IN_PROGRESS": 1}, nil))
		approved.ReviewDecision = "APPROVED"
		f.with(func() {
			f.open = []authoredPR{approved, openAt(2, testNow.Add(-time.Hour))}
			f.convos["AP_02"] = convo([]conversationItem{item(person("sam"), t1)}, nil)
		})
		r := startAuthored(t, newAuthored(f, time.Hour))
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })
		updates := r.stop(t)

		first, second := updates[0][0], updates[0][1]
		assertEqual(t, "widgets#1", first.Detail)
		assertEqual(t, task.Status{Symbol: "checkmark", Tone: task.ToneSuccess}, *first.Lead)
		assertEqual(t, task.Readout{Symbol: "circle.fill", Tone: task.ToneWarning, Text: "11/12"}, first.Readouts[0])
		assertEqual(t, 1, len(first.Readouts))
		assertEqual(t, "widgets#2", second.Detail)
		assertEqual(t, task.Status{Symbol: "bubble.left.fill", Tone: task.ToneAccent}, *second.Lead)
		assertEqual(t, task.Readout{Symbol: "bubble.left.fill", Tone: task.ToneNeutral, Text: "1"}, second.Readouts[0])
		assertEqual(t, 1, len(second.Readouts))
	})

	t.Run("publishes again when only the check counts change", func(t *testing.T) {
		f := newAuthoredAPI(t)
		f.with(func() {
			f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour)).withRollup(rollup(map[string]int{"SUCCESS": 5, "IN_PROGRESS": 2}, nil))}
		})
		r := startAuthored(t, newAuthored(f, 5*time.Millisecond))
		waitFor(t, "a first update", func() bool { return r.updateCount() >= 1 })
		f.with(func() {
			f.open[0] = f.open[0].withRollup(rollup(map[string]int{"SUCCESS": 6, "IN_PROGRESS": 1}, nil))
		})
		waitFor(t, "an update for the new counts", func() bool { return r.updateCount() >= 2 })
		updates := r.stop(t)

		assertEqual(t, details(updates)[0][0], details(updates)[1][0])
		assertEqual(t, "5/7", updates[0][0].Readouts[0].Text)
		assertEqual(t, "6/7", updates[1][0].Readouts[0].Text)
	})

	t.Run("lists no merged pull request", func(t *testing.T) {
		f := newAuthoredAPI(t)
		r := startAuthored(t, newAuthored(f, time.Hour))
		waitFor(t, "a list", func() bool { return f.listCount() >= 1 })
		r.stop(t)
		f.read(func() { assertEqual(t, 1, len(f.lists)) })
	})
}
