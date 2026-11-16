package slack

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/coder/websocket"

	"github.com/craigmjohnston/roundup/task"
)

const laterSource = "slack-later"

//go:embed slack.svg
var slackSVG []byte

// Later is the task source for a workspace's Slack "Later" (saved for later) items.
type Later struct {
	ws        Workspace
	client    *Client
	cachePath string

	authenticate func(ctx context.Context, ws Workspace) error

	// OnDisconnect, when set, is told why the event connection ended before Run reconnects.
	OnDisconnect func(err error)

	// OnLoaded, when set, is called each time every task being shown has had its lookups finish.
	OnLoaded func()

	publishEvery time.Duration
	pingEvery    time.Duration
	pingTimeout  time.Duration
	minBackoff   time.Duration
	maxBackoff   time.Duration
}

var (
	_ task.Source = (*Later)(nil)
	_ task.SignIn = (*Later)(nil)
)

func NewLater(ws Workspace) *Later {
	return &Later{
		ws:           ws,
		client:       NewClient(ws),
		cachePath:    defaultCachePath(ws),
		authenticate: Authenticate,
		publishEvery: 200 * time.Millisecond,
		pingEvery:    30 * time.Second,
		pingTimeout:  10 * time.Second,
		minBackoff:   time.Second,
		maxBackoff:   time.Minute,
	}
}

func (l *Later) Info() task.Info {
	return task.Info{ID: laterSource, Name: "Slack", Icon: "slack", Icons: map[string]task.Icon{"slack": {SVG: slackSVG}}}
}

// SignIn signs in through Chrome, after which Run can be started again.
func (l *Later) SignIn(ctx context.Context) error { return l.authenticate(ctx, l.ws) }

// Run keeps update supplied with the open Later items until ctx ends, then returns nil.
// It reconnects after a lost connection and returns an error wrapping task.ErrSignedOut when the session is no longer valid.
func (l *Later) Run(ctx context.Context, update func([]task.Task)) error {
	cache := loadCache(l.cachePath)
	backoff := l.minBackoff
	for {
		connected, err := l.runConnection(ctx, cache, update)
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, task.ErrSignedOut):
			return err
		}
		if l.OnDisconnect != nil {
			l.OnDisconnect(err)
		}
		if connected {
			backoff = l.minBackoff
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, l.maxBackoff)
	}
}

// runConnection opens the event connection, loads every open item and applies events until the connection fails.
// It publishes as soon as the items are loaded, then again as lookups fill in their text.
// connected reports whether the first load finished.
func (l *Later) runConnection(ctx context.Context, cache *cache, update func([]task.Task)) (connected bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, err := l.client.connectEvents(ctx)
	if err != nil {
		return false, err
	}
	defer conn.CloseNow()

	events := make(chan []byte, 256)
	failed := make(chan error, 2)
	go readEvents(ctx, conn, events, failed)
	go pingEvery(ctx, conn, l.pingEvery, l.pingTimeout, failed)

	id, err := l.client.WhoAmI(ctx)
	if err != nil {
		return false, err
	}
	items, err := l.loadItems(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = cache.save() }()

	v := newView(id.TeamID, items, cache)
	msgs := make(chan workerMsg, 64)
	running := false
	var lastPublish time.Time
	var flush <-chan time.Time

	publish := func() {
		flush = nil
		lastPublish = time.Now()
		update(v.tasks())
	}
	publishSoon := func() {
		wait := l.publishEvery - time.Since(lastPublish)
		switch {
		case wait <= 0:
			publish()
		case flush == nil:
			flush = time.After(wait)
		}
	}
	startWork := func() bool {
		if running {
			return true
		}
		req, ok := v.request()
		if !ok {
			return false
		}
		running = true
		go func() {
			err := runLookups(ctx, l.client, req, func(u lookupUpdate) {
				select {
				case msgs <- workerMsg{update: u}:
				case <-ctx.Done():
				}
			})
			select {
			case msgs <- workerMsg{done: true, err: err}:
			case <-ctx.Done():
			}
		}()
		return true
	}
	loaded := func() {
		if l.OnLoaded != nil {
			l.OnLoaded()
		}
	}

	publish()
	if !startWork() {
		loaded()
	}

	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case err := <-failed:
			return true, err
		case <-flush:
			publish()
		case m := <-msgs:
			if !m.done {
				v.merge(m.update)
				publishSoon()
				continue
			}
			running = false
			if m.err != nil {
				return true, m.err
			}
			v.finish()
			_ = cache.save()
			publish()
			if !startWork() {
				loaded()
			}
		case data := <-events:
			ev, ok := parseSavedEvent(data)
			if !ok {
				continue
			}
			var changed bool
			v.items, changed = applyEvent(v.items, ev)
			if !changed {
				continue
			}
			publish()
			startWork()
		}
	}
}

type workerMsg struct {
	update lookupUpdate
	done   bool
	err    error
}

// loadItems pages through saved.list and keeps the items that are not completed.
func (l *Later) loadItems(ctx context.Context) ([]savedItem, error) {
	var items []savedItem
	cursor := ""
	for {
		params := url.Values{"limit": {"50"}}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		body, err := l.client.Call(ctx, "saved.list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			SavedItems       []savedItem `json:"saved_items"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decoding saved.list: %w", err)
		}
		for _, it := range page.SavedItems {
			if it.State != stateCompleted {
				items = append(items, it)
			}
		}
		cursor = page.ResponseMetadata.NextCursor
		if cursor == "" {
			return items, nil
		}
	}
}

func readEvents(ctx context.Context, conn *websocket.Conn, events chan<- []byte, failed chan<- error) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			failed <- fmt.Errorf("reading event: %w", err)
			return
		}
		select {
		case events <- data:
		case <-ctx.Done():
			return
		}
	}
}

func pingEvery(ctx context.Context, conn *websocket.Conn, every, timeout time.Duration, failed chan<- error) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		pingCtx, cancel := context.WithTimeout(ctx, timeout)
		err := conn.Ping(pingCtx)
		cancel()
		if err != nil {
			failed <- fmt.Errorf("pinging event connection: %w", err)
			return
		}
	}
}

func sortTasks(tasks []task.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.Due.IsZero() != b.Due.IsZero() {
			return !a.Due.IsZero()
		}
		if !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		return a.Key < b.Key
	})
}
