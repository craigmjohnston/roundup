package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

type fakeAPI struct {
	srv *httptest.Server

	mu          sync.Mutex
	pages       map[string][][]pullRequest
	status      int
	headers     map[string]string
	requests    []apiRequest
	gate        chan struct{}
	details     []detailRequest
	detailFails bool
}

type apiRequest struct {
	search string
	after  string
	auth   string
}

type detailRequest struct{ ids []string }

func (f *fakeAPI) detailRequests() []detailRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]detailRequest(nil), f.details...)
}

func (f *fakeAPI) failDetail(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detailFails = on
}

func (f *fakeAPI) serveDetail(w http.ResponseWriter, ids []string) {
	f.details = append(f.details, detailRequest{ids})
	if f.detailFails {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	var nodes []map[string]any
	for _, page := range f.pages[repliesSearch] {
		for _, pr := range page {
			if slices.Contains(ids, pr.ID) {
				nodes = append(nodes, map[string]any{"id": pr.ID, "reviewThreads": pr.ReviewThreads})
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"nodes": nodes, "rateLimit": map[string]any{"cost": 2}}})
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{pages: map[string][][]pullRequest{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query     string `json:"query"`
		Variables struct {
			Q     string   `json:"q"`
			After string   `json:"after"`
			IDs   []string `json:"ids"`
		} `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if body.Query == threadsQuery {
		f.serveDetail(w, body.Variables.IDs)
		return
	}
	f.requests = append(f.requests, apiRequest{body.Variables.Q, body.Variables.After, r.Header.Get("Authorization")})
	for k, v := range f.headers {
		w.Header().Set(k, v)
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	base, _, _ := strings.Cut(body.Variables.Q, " updated:>=")
	pages := f.pages[base]
	page := 0
	if body.Variables.After != "" {
		page = int(body.Variables.After[len("cursor")] - '0')
	}
	var data searchData
	data.Viewer.Login = me
	if page < len(pages) {
		data.Search.Nodes = slices.Clone(pages[page])
		if base == repliesSearch {
			for i := range data.Search.Nodes {
				data.Search.Nodes[i].ReviewThreads.Nodes = nil
			}
		}
	}
	if page+1 < len(pages) {
		data.Search.PageInfo.HasNextPage = true
		data.Search.PageInfo.EndCursor = "cursor" + string(rune('0'+page+1))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (f *fakeAPI) set(search string, pages ...[]pullRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[search] = pages
}

func (f *fakeAPI) fail(status int, headers map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.headers = status, headers
}

func (f *fakeAPI) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeAPI) recorded() []apiRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]apiRequest(nil), f.requests...)
}

type run struct {
	mu      sync.Mutex
	updates [][]task.Task
	done    chan error
	cancel  context.CancelFunc
	src     *Reviews
}

func start(t *testing.T, f *fakeAPI, token func(context.Context) (string, error)) *run {
	t.Helper()
	return startEvery(t, f, token, 5*time.Millisecond)
}

func startEvery(t *testing.T, f *fakeAPI, token func(context.Context) (string, error), interval time.Duration) *run {
	t.Helper()
	src := newSource(f, token, interval)
	return startSource(t, src)
}

func newSource(f *fakeAPI, token func(context.Context) (string, error), interval time.Duration) *Reviews {
	return &Reviews{baseURL: f.srv.URL, token: token, interval: interval, client: f.srv.Client(), now: func() time.Time { return time.Time{} }, refresh: make(chan struct{}, 1)}
}

func startSource(t *testing.T, src *Reviews) *run {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{done: make(chan error, 1), cancel: cancel, src: src}
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

func (r *run) updateCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.updates)
}

func (r *run) stop(t *testing.T) [][]task.Task {
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
	return append([][]task.Task(nil), r.updates...)
}

func staticToken(context.Context) (string, error) { return "secret", nil }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestReviewsRun_Polling(t *testing.T) {
	t.Run("publishes the first result even when it is empty", func(t *testing.T) {
		f := newFakeAPI(t)
		r := start(t, f, staticToken)
		waitFor(t, "two polls", func() bool { return f.requestCount() >= 4 })

		updates := r.stop(t)
		if want, got := 1, len(updates); want != got {
			t.Fatalf("updates: want %d, got %d", want, got)
		}
		if len(updates[0]) != 0 {
			t.Errorf("want an empty snapshot, got %v", updates[0])
		}
	})

	t.Run("calls update once for an unchanged set and again when it changes", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1)})
		r := start(t, f, staticToken)
		waitFor(t, "three polls", func() bool { return f.requestCount() >= 6 })

		f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1), newPR(8, "sam").requestedFrom(user(me), t2)})
		n := f.requestCount()
		waitFor(t, "polls after the change", func() bool { return f.requestCount() >= n+6 })

		updates := r.stop(t)
		var sizes []int
		for _, u := range updates {
			sizes = append(sizes, len(u))
		}
		assertTasks(t, []int{1, 2}, sizes)
	})

	t.Run("sends the token and both searches on each poll", func(t *testing.T) {
		f := newFakeAPI(t)
		r := start(t, f, staticToken)
		waitFor(t, "a poll", func() bool { return f.requestCount() >= 2 })
		r.stop(t)

		reqs := f.recorded()
		want := []apiRequest{{withCutoff(requestedSearch, time.Time{}), "", "Bearer secret"}, {withCutoff(repliesSearch, time.Time{}), "", "Bearer secret"}}
		assertTasks(t, want, reqs[:2])
	})

	t.Run("follows search pagination", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(requestedSearch,
			[]pullRequest{newPR(7, "sam").requestedFrom(user(me), t1)},
			[]pullRequest{newPR(8, "sam").requestedFrom(user(me), t2)},
		)
		r := start(t, f, staticToken)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })

		updates := r.stop(t)
		assertTasks(t, []string{"acme/widgets#7", "acme/widgets#8"}, keysOf(updates[0]))
		reqs := f.recorded()
		assertTasks(t, []apiRequest{{withCutoff(requestedSearch, time.Time{}), "cursor1", "Bearer secret"}}, reqs[1:2])
	})

	t.Run("keeps the last snapshot when a poll fails and carries on afterwards", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1)})
		r := start(t, f, staticToken)
		waitFor(t, "a poll", func() bool { return f.requestCount() >= 2 })

		f.fail(http.StatusBadGateway, nil)
		n := f.requestCount()
		waitFor(t, "failed polls", func() bool { return f.requestCount() >= n+3 })
		f.fail(0, nil)
		f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1)})
		n = f.requestCount()
		waitFor(t, "recovered polls", func() bool { return f.requestCount() >= n+4 })

		updates := r.stop(t)
		if want, got := 1, len(updates); want != got {
			t.Errorf("updates: want %d, got %d", want, got)
		}
	})

	t.Run("waits for Retry-After when rate limited", func(t *testing.T) {
		f := newFakeAPI(t)
		f.fail(http.StatusTooManyRequests, map[string]string{"Retry-After": "30"})
		r := start(t, f, staticToken)
		waitFor(t, "a poll", func() bool { return f.requestCount() >= 1 })
		time.Sleep(60 * time.Millisecond)

		updates := r.stop(t)
		if want, got := 1, f.requestCount(); want != got {
			t.Errorf("requests: want %d, got %d", want, got)
		}
		if len(updates) != 0 {
			t.Errorf("want no updates, got %v", updates)
		}
	})

	t.Run("ends with an error on 401", func(t *testing.T) {
		f := newFakeAPI(t)
		f.fail(http.StatusUnauthorized, nil)
		r := start(t, f, staticToken)

		select {
		case err := <-r.done:
			if !errors.Is(err, errUnauthorized) {
				t.Errorf("want errUnauthorized, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return")
		}
	})

	t.Run("ends with an error when there is no token", func(t *testing.T) {
		f := newFakeAPI(t)
		r := start(t, f, func(context.Context) (string, error) { return "", errNoToken })

		select {
		case err := <-r.done:
			if !errors.Is(err, errNoToken) {
				t.Errorf("want errNoToken, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return")
		}
		if want, got := 0, f.requestCount(); want != got {
			t.Errorf("requests: want %d, got %d", want, got)
		}
	})
}

func TestReviewsRefresh(t *testing.T) {
	const hour = time.Hour

	t.Run("polls straight away instead of waiting out the interval", func(t *testing.T) {
		f := newFakeAPI(t)
		r := startEvery(t, f, staticToken, hour)
		waitFor(t, "the first poll", func() bool { return f.requestCount() >= 2 })

		r.src.Refresh()
		waitFor(t, "the refresh poll", func() bool { return f.requestCount() >= 4 })
		r.stop(t)
	})

	t.Run("collapses a burst of calls into one extra poll", func(t *testing.T) {
		f := newFakeAPI(t)
		f.gate = make(chan struct{})
		r := startEvery(t, f, staticToken, hour)
		time.Sleep(20 * time.Millisecond)

		for range 20 {
			r.src.Refresh()
		}
		close(f.gate)
		waitFor(t, "the refresh poll", func() bool { return f.requestCount() >= 4 })
		time.Sleep(50 * time.Millisecond)
		r.stop(t)
		if want, got := 4, f.requestCount(); want != got {
			t.Errorf("requests: want %d, got %d", want, got)
		}
	})

	t.Run("drops a call made before Run starts", func(t *testing.T) {
		f := newFakeAPI(t)
		src := newSource(f, staticToken, hour)
		src.Refresh()
		r := startSource(t, src)
		waitFor(t, "the first poll", func() bool { return f.requestCount() >= 2 })
		time.Sleep(50 * time.Millisecond)
		r.stop(t)
		if want, got := 2, f.requestCount(); want != got {
			t.Errorf("requests: want %d, got %d", want, got)
		}
	})

	t.Run("does not cut a rate limit wait short", func(t *testing.T) {
		f := newFakeAPI(t)
		f.fail(http.StatusTooManyRequests, map[string]string{"Retry-After": "30"})
		r := startEvery(t, f, staticToken, hour)
		waitFor(t, "a poll", func() bool { return f.requestCount() >= 1 })

		r.src.Refresh()
		time.Sleep(60 * time.Millisecond)
		r.stop(t)
		if want, got := 1, f.requestCount(); want != got {
			t.Errorf("requests: want %d, got %d", want, got)
		}
	})

	t.Run("publishes a changed set and keeps the snapshot when the poll fails", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1)})
		r := startEvery(t, f, staticToken, hour)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })

		f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1), newPR(8, "sam").requestedFrom(user(me), t2)})
		r.src.Refresh()
		waitFor(t, "the changed set", func() bool { return r.updateCount() >= 2 })

		f.fail(http.StatusBadGateway, nil)
		n := f.requestCount()
		r.src.Refresh()
		waitFor(t, "the failed poll", func() bool { return f.requestCount() > n })
		time.Sleep(30 * time.Millisecond)

		updates := r.stop(t)
		if want, got := 2, len(updates); want != got {
			t.Errorf("updates: want %d, got %d", want, got)
		}
	})
}

func replyPR(number int) pullRequest {
	return newPR(number, "sam").withThreads(thread(false,
		commentSpec{me, t1, "https://github.com/acme/widgets/pull/1#c1"},
		commentSpec{"sam", t2, "https://github.com/acme/widgets/pull/1#c2"}))
}

func detailIDs(reqs []detailRequest) []string {
	var out []string
	for _, r := range reqs {
		out = append(out, strings.Join(r.ids, ","))
	}
	return out
}

func TestReviewsRun_ThreadCache(t *testing.T) {
	refreshAndWait := func(t *testing.T, r *run, f *fakeAPI) {
		t.Helper()
		n := f.requestCount()
		r.src.Refresh()
		waitFor(t, "a poll", func() bool { return f.requestCount() >= n+2 })
	}

	t.Run("fetches threads for a new pull request and publishes its reply task", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(repliesSearch, []pullRequest{replyPR(7)})
		r := startEvery(t, f, staticToken, time.Hour)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })

		updates := r.stop(t)
		assertTasks(t, []string{"acme/widgets#7"}, keysOf(updates[0]))
		assertTasks(t, []string{"PR_07"}, detailIDs(f.detailRequests()))
	})

	t.Run("skips the detail query when nothing listed has changed", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(repliesSearch, []pullRequest{replyPR(7)})
		r := startEvery(t, f, staticToken, time.Hour)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })
		refreshAndWait(t, r, f)
		refreshAndWait(t, r, f)
		r.stop(t)

		assertTasks(t, []string{"PR_07"}, detailIDs(f.detailRequests()))
	})

	t.Run("fetches only new pull requests and ones whose updatedAt changed", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(repliesSearch, []pullRequest{replyPR(7), replyPR(9)})
		r := startEvery(t, f, staticToken, time.Hour)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })

		changed := replyPR(7)
		changed.UpdatedAt = t3.Add(time.Hour)
		f.set(repliesSearch, []pullRequest{changed, replyPR(8), replyPR(9)})
		refreshAndWait(t, r, f)
		waitFor(t, "the second detail query", func() bool { return len(f.detailRequests()) >= 2 })
		r.stop(t)

		assertTasks(t, []string{"PR_07,PR_09", "PR_07,PR_08"}, detailIDs(f.detailRequests()))
	})

	t.Run("batches ids five to a request", func(t *testing.T) {
		f := newFakeAPI(t)
		var prs []pullRequest
		for n := 1; n <= 7; n++ {
			prs = append(prs, replyPR(n))
		}
		f.set(repliesSearch, prs)
		r := startEvery(t, f, staticToken, time.Hour)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })
		r.stop(t)

		var sizes []int
		for _, d := range f.detailRequests() {
			sizes = append(sizes, len(d.ids))
		}
		assertTasks(t, []int{5, 2}, sizes)
	})

	t.Run("evicts a pull request that drops out of the list", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(repliesSearch, []pullRequest{replyPR(7)})
		r := startEvery(t, f, staticToken, time.Hour)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })

		f.set(repliesSearch)
		refreshAndWait(t, r, f)
		f.set(repliesSearch, []pullRequest{replyPR(7)})
		refreshAndWait(t, r, f)
		waitFor(t, "the second detail query", func() bool { return len(f.detailRequests()) >= 2 })
		r.stop(t)

		assertTasks(t, []string{"PR_07", "PR_07"}, detailIDs(f.detailRequests()))
	})

	t.Run("keeps the last snapshot when the detail query fails and retries it next poll", func(t *testing.T) {
		f := newFakeAPI(t)
		f.set(repliesSearch, []pullRequest{replyPR(7)})
		r := startEvery(t, f, staticToken, time.Hour)
		waitFor(t, "an update", func() bool { return r.updateCount() >= 1 })

		f.failDetail(true)
		changed := replyPR(7)
		changed.UpdatedAt = t3.Add(time.Hour)
		f.set(repliesSearch, []pullRequest{changed})
		refreshAndWait(t, r, f)
		waitFor(t, "the failed detail query", func() bool { return len(f.detailRequests()) >= 2 })
		f.failDetail(false)
		refreshAndWait(t, r, f)
		waitFor(t, "the retried detail query", func() bool { return len(f.detailRequests()) >= 3 })
		updates := r.stop(t)

		assertTasks(t, []string{"PR_07", "PR_07", "PR_07"}, detailIDs(f.detailRequests()))
		if want, got := 1, len(updates); want != got {
			t.Errorf("updates: want %d, got %d", want, got)
		}
	})

	t.Run("limits both searches to the stale window from the injected clock", func(t *testing.T) {
		f := newFakeAPI(t)
		src := newSource(f, staticToken, time.Hour)
		src.now = func() time.Time { return time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC) }
		r := startSource(t, src)
		waitFor(t, "a poll", func() bool { return f.requestCount() >= 2 })
		r.stop(t)

		var searches []string
		for _, req := range f.recorded()[:2] {
			searches = append(searches, req.search)
		}
		want := []string{requestedSearch + " updated:>=2026-10-29", repliesSearch + " updated:>=2026-10-29"}
		assertTasks(t, want, searches)
	})
}

func TestGraphQL_RateLimit(t *testing.T) {
	serve := func(t *testing.T, body string) *Reviews {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		t.Cleanup(srv.Close)
		return &Reviews{baseURL: srv.URL, client: srv.Client()}
	}

	t.Run("decodes cost, remaining and reset time", func(t *testing.T) {
		r := serve(t, `{"data":{"rateLimit":{"cost":3,"remaining":4990,"resetAt":"2026-11-12T10:00:00Z"}}}`)
		var got searchData
		if err := r.graphQL(context.Background(), "secret", "q", nil, &got); err != nil {
			t.Fatal(err)
		}
		if got.RateLimit.Cost != 3 || got.RateLimit.Remaining != 4990 || got.RateLimit.ResetAt.IsZero() {
			t.Errorf("unexpected rate limit %+v", got.RateLimit)
		}
	})

	t.Run("accepts a response without it", func(t *testing.T) {
		r := serve(t, `{"data":{"viewer":{"login":"me"}}}`)
		var got searchData
		if err := r.graphQL(context.Background(), "secret", "q", nil, &got); err != nil {
			t.Fatal(err)
		}
		if got.RateLimit.Cost != 0 || got.Viewer.Login != "me" {
			t.Errorf("unexpected data %+v", got)
		}
	})
}

func TestRetryAfter(t *testing.T) {
	now := time.Unix(1000, 0)
	tests := []struct {
		name    string
		headers map[string]string
		want    time.Duration
	}{
		{"uses Retry-After seconds", map[string]string{"Retry-After": "12"}, 12 * time.Second},
		{"uses the reset time when no requests remain", map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1090"}, 90 * time.Second},
		{"ignores the reset time while requests remain", map[string]string{"X-RateLimit-Remaining": "5", "X-RateLimit-Reset": "1090"}, 0},
		{"is zero without headers", nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			if got := retryAfter(h, now); tc.want != got {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func keysOf(tasks []task.Task) []string {
	var out []string
	for _, tk := range tasks {
		out = append(out, tk.Key)
	}
	return out
}

func TestReviewsInfo(t *testing.T) {
	info := NewReviews().Info()
	assertEqual(t, "github-reviews", info.ID)
	assertEqual(t, "GitHub", info.Name)
	icon := info.Icons[info.Icon]
	assertEqual(t, true, icon.Template && len(icon.SVG) > 0)
	assertEqual(t, 0, len(info.Sections))
}
