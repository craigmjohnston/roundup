// Package github holds the task sources for the signed-in user's pull requests: those awaiting their review and their own.
package github

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

const reviewsSource = "github-reviews"

// Reviews is the task source for pull requests awaiting the user's review or answer.
type Reviews struct {
	// BotLogins are accounts that act for automation but appear as ordinary users.
	BotLogins []string

	baseURL  string
	token    func(ctx context.Context) (string, error)
	interval time.Duration
	client   *http.Client
	now      func() time.Time
	refresh  chan struct{}
	threads  map[string]cachedThreads
}

type cachedThreads struct {
	updatedAt time.Time
	threads   []reviewThread
}

var (
	_ task.Source    = (*Reviews)(nil)
	_ task.Refresher = (*Reviews)(nil)
)

func NewReviews() *Reviews {
	c := newConn()
	return &Reviews{baseURL: c.baseURL, token: c.token, interval: c.interval, client: c.client, now: c.now, refresh: c.refresh}
}

func (r *Reviews) Info() task.Info { return githubInfo(reviewsSource) }

// Refresh asks Run to poll now. It never blocks, and requests made while a poll is running or pending become one poll.
func (r *Reviews) Refresh() { r.conn().Refresh() }

// Run polls until ctx ends, then returns nil.
// It keeps the last snapshot when a poll fails and returns an error when gh has no token or GitHub rejects it.
func (r *Reviews) Run(ctx context.Context, update func([]task.Task)) error {
	return r.conn().run(ctx, r.poll, update)
}

func (r *Reviews) conn() *conn {
	return &conn{baseURL: r.baseURL, token: r.token, interval: r.interval, client: r.client, now: r.now, refresh: r.refresh}
}

func (r *Reviews) poll(ctx context.Context, token string) ([]task.Task, error) {
	now := r.now()
	requested, viewer, err := r.searchAll(ctx, token, requestedQuery, withCutoff(requestedSearch, now))
	if err != nil {
		return nil, fmt.Errorf("searching review requests: %w", err)
	}
	reviewed, _, err := r.searchAll(ctx, token, repliesQuery, withCutoff(repliesSearch, now))
	if err != nil {
		return nil, fmt.Errorf("searching reviewed pull requests: %w", err)
	}
	reviewed = fresh(reviewed, now)
	if err := r.loadThreads(ctx, token, reviewed); err != nil {
		return nil, fmt.Errorf("loading review threads: %w", err)
	}
	for i := range reviewed {
		reviewed[i].ReviewThreads.Nodes = r.threads[reviewed[i].ID].threads
	}
	return buildTasks(viewer, r.BotLogins, fresh(requested, now), reviewed), nil
}

// loadThreads fetches review threads for pull requests that are new or updated since cached, and evicts the ones no longer listed.
func (r *Reviews) loadThreads(ctx context.Context, token string, prs []pullRequest) error {
	if r.threads == nil {
		r.threads = map[string]cachedThreads{}
	}
	var stale []pullRequest
	for _, pr := range prs {
		if c, ok := r.threads[pr.ID]; !ok || !c.updatedAt.Equal(pr.UpdatedAt) {
			stale = append(stale, pr)
		}
	}
	for batch := range slices.Chunk(stale, threadsBatch) {
		updated := map[string]time.Time{}
		ids := make([]string, len(batch))
		for i, pr := range batch {
			ids[i] = pr.ID
			updated[pr.ID] = pr.UpdatedAt
		}
		var data threadsData
		if err := r.graphQL(ctx, token, threadsQuery, map[string]any{"ids": ids}, &data); err != nil {
			return err
		}
		for _, n := range data.Nodes {
			if n != nil {
				r.threads[n.ID] = cachedThreads{updatedAt: updated[n.ID], threads: n.ReviewThreads.Nodes}
			}
		}
	}
	listed := map[string]bool{}
	for _, pr := range prs {
		listed[pr.ID] = true
	}
	maps.DeleteFunc(r.threads, func(id string, _ cachedThreads) bool { return !listed[id] })
	return nil
}

// searchAll pages through one search and also returns the signed-in user's login.
func (r *Reviews) searchAll(ctx context.Context, token, query, search string) (prs []pullRequest, viewer string, err error) {
	after := ""
	for {
		var data searchData
		vars := map[string]any{"q": search}
		if after != "" {
			vars["after"] = after
		}
		if err := r.graphQL(ctx, token, query, vars, &data); err != nil {
			return nil, "", err
		}
		viewer = data.Viewer.Login
		prs = append(prs, data.Search.Nodes...)
		if !data.Search.PageInfo.HasNextPage {
			return prs, viewer, nil
		}
		after = data.Search.PageInfo.EndCursor
	}
}

func (r *Reviews) graphQL(ctx context.Context, token, query string, vars map[string]any, out any) error {
	return r.conn().graphQL(ctx, token, query, vars, out)
}
