package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/craigmjohnston/roundup/task"
)

type recordedCall struct {
	method string
	params url.Values
}

// fakeSlack serves the Web API methods and the event websocket the Later source uses.
type fakeSlack struct {
	srv   *httptest.Server
	conns chan *websocket.Conn
	done  chan struct{}

	mu       sync.Mutex
	calls    []recordedCall
	items    []map[string]any
	pageSize int
	channels map[string]map[string]any
	users    map[string]string
	userAs   map[string]map[string]any
	profiles map[string]map[string]any
	history  map[string]map[string]any
	replies  map[string]map[string]any
	authErr  map[string]string
	silent   bool

	cachePath string
	gate      chan struct{}
	left      map[string]bool
	notInBulk map[string]bool
	omitBulk  map[string]bool
	convPage  int
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{
		conns:    make(chan *websocket.Conn, 16),
		done:     make(chan struct{}),
		pageSize: 50,
		channels: map[string]map[string]any{},
		users:    map[string]string{},
		userAs:   map[string]map[string]any{},
		profiles: map[string]map[string]any{},
		history:  map[string]map[string]any{},
		replies:  map[string]map[string]any{},
		authErr:  map[string]string{},

		cachePath: filepath.Join(t.TempDir(), "roundup", "slack-acme.json"),
		left:      map[string]bool{},
		notInBulk: map[string]bool{},
		omitBulk:  map[string]bool{},
		convPage:  1000,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		close(f.done)
		f.srv.Close()
	})
	return f
}

func (f *fakeSlack) client() *Client {
	return testClient(f.srv.URL, session{Token: "xoxc-test", Cookie: "xoxd-test"})
}

func (f *fakeSlack) later() *Later {
	return &Later{
		client:       f.client(),
		cachePath:    f.cachePath,
		publishEvery: time.Millisecond,
		pingEvery:    time.Hour,
		pingTimeout:  time.Second,
		minBackoff:   5 * time.Millisecond,
		maxBackoff:   20 * time.Millisecond,
	}
}

func (f *fakeSlack) setItems(items ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = items
}

func (f *fakeSlack) setSilent(silent bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.silent = silent
}

func (f *fakeSlack) failWith(method, slackError string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authErr[method] = slackError
}

// holdMessages makes message lookups wait, unrecorded, until the returned function is called.
func (f *fakeSlack) holdMessages() (release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = make(chan struct{})
	gate := f.gate
	return func() { close(gate) }
}

func (f *fakeSlack) callsTo(method string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedCall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		f.serveSocket(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/api/")

	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil && (strings.HasPrefix(method, "conversations.") && method != "conversations.info" || method == "messages.list") {
		select {
		case <-gate:
		case <-f.done:
			return
		}
	}

	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{method: method, params: r.PostForm})
	slackErr := f.authErr[method]
	f.mu.Unlock()

	var body any
	if slackErr != "" {
		body = map[string]any{"ok": false, "error": slackErr}
	} else {
		body = f.respond(method, r.PostForm)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

func (f *fakeSlack) serveSocket(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	f.conns <- c
	f.mu.Lock()
	silent := f.silent
	f.mu.Unlock()
	if silent {
		<-f.done
		return
	}
	<-c.CloseRead(context.Background()).Done()
}

func (f *fakeSlack) respond(method string, p url.Values) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "rtm.connect":
		return map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws"}
	case "auth.test":
		return map[string]any{"ok": true, "user": "me", "team": "Acme", "team_id": "T1", "url": "https://acme.slack.com/"}
	case "saved.list":
		start := 0
		if c := p.Get("cursor"); c != "" {
			start, _ = strconv.Atoi(c)
		}
		end := min(start+f.pageSize, len(f.items))
		next := ""
		if end < len(f.items) {
			next = strconv.Itoa(end)
		}
		return map[string]any{"ok": true, "saved_items": f.items[start:end], "response_metadata": map[string]any{"next_cursor": next}}
	case "conversations.info":
		ch, ok := f.channels[p.Get("channel")]
		if !ok {
			return map[string]any{"ok": false, "error": "channel_not_found"}
		}
		return map[string]any{"ok": true, "channel": ch}
	case "messages.list":
		var ids []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		if err := json.Unmarshal([]byte(p.Get("message_ids")), &ids); err != nil || len(ids) > 20 {
			return map[string]any{"ok": false, "error": "too_many_channels"}
		}
		out := map[string]any{}
		for _, e := range ids {
			list := []any{}
			for _, ts := range e.Timestamps {
				key := e.Channel + "/" + ts
				if f.omitBulk[key] {
					continue
				}
				if m, ok := f.history[key]; ok {
					list = append(list, m)
				} else if m, ok := f.replies[key]; ok {
					list = append(list, m)
				}
			}
			out[e.Channel] = list
		}
		return map[string]any{"ok": true, "messages": out, "messages_data": map[string]any{}}
	case "users.conversations":
		return f.conversationsPage(p)
	case "users.info":
		if ids := p.Get("users"); ids != "" {
			list := []any{}
			for _, id := range strings.Split(ids, ",") {
				if name, ok := f.users[id]; ok && !f.notInBulk[id] {
					list = append(list, f.userJSON(id, name))
				}
			}
			return map[string]any{"ok": true, "users": list}
		}
		name, ok := f.users[p.Get("user")]
		if !ok {
			return map[string]any{"ok": false, "error": "user_not_found"}
		}
		return map[string]any{"ok": true, "user": f.userJSON(p.Get("user"), name)}
	case "conversations.history":
		return messagesResponse(f.history, p)
	case "conversations.replies":
		return messagesResponse(f.replies, p)
	}
	return map[string]any{"ok": false, "error": "unknown_method"}
}

func (f *fakeSlack) userJSON(id, name string) map[string]any {
	profile := map[string]any{"display_name": name}
	for k, v := range f.profiles[id] {
		profile[k] = v
	}
	u := map[string]any{"id": id, "name": "login", "profile": profile}
	for k, v := range f.userAs[id] {
		u[k] = v
	}
	return u
}

// conversationsPage lists the channels the user is in, in id order, paged by cursor.
func (f *fakeSlack) conversationsPage(p url.Values) any {
	var ids []string
	for id := range f.channels {
		if !f.left[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	start := 0
	if c := p.Get("cursor"); c != "" {
		start, _ = strconv.Atoi(c)
	}
	end := min(start+f.convPage, len(ids))
	next := ""
	if end < len(ids) {
		next = strconv.Itoa(end)
	}
	list := []any{}
	for _, id := range ids[start:end] {
		ch := map[string]any{"id": id}
		for k, v := range f.channels[id] {
			ch[k] = v
		}
		list = append(list, ch)
	}
	return map[string]any{"ok": true, "channels": list, "response_metadata": map[string]any{"next_cursor": next}}
}

func messagesResponse(store map[string]map[string]any, p url.Values) any {
	msgs := []any{}
	if m, ok := store[p.Get("channel")+"/"+p.Get("latest")]; ok {
		msgs = append(msgs, m)
	}
	return map[string]any{"ok": true, "messages": msgs}
}

func (f *fakeSlack) nextConn(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case c := <-f.conns:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no event connection opened")
		return nil
	}
}

func sendEvent(t *testing.T, c *websocket.Conn, ev map[string]any) {
	t.Helper()
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

type running struct {
	snapshots chan []task.Task
	loaded    chan struct{}
	result    chan error
	cancel    context.CancelFunc
}

func runSource(t *testing.T, src task.Source) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{snapshots: make(chan []task.Task, 256), loaded: make(chan struct{}, 16), result: make(chan error, 1), cancel: cancel}
	if l, ok := src.(*Later); ok {
		l.OnLoaded = func() { r.loaded <- struct{}{} }
	}
	go func() {
		r.result <- src.Run(ctx, func(tasks []task.Task) { r.snapshots <- tasks })
	}()
	t.Cleanup(cancel)
	return r
}

func (r *running) next(t *testing.T) []task.Task {
	t.Helper()
	select {
	case s := <-r.snapshots:
		return s
	case err := <-r.result:
		t.Fatalf("Run ended before the next snapshot: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no snapshot within 5s")
	}
	return nil
}

// settle waits until the source has finished its lookups and returns the last snapshot.
func (r *running) settle(t *testing.T) []task.Task {
	t.Helper()
	select {
	case <-r.loaded:
	case err := <-r.result:
		t.Fatalf("Run ended before lookups finished: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("lookups did not finish within 5s")
	}
	var last []task.Task
	for {
		select {
		case last = <-r.snapshots:
		default:
			return last
		}
	}
}

// until returns the first snapshot that satisfies ok.
func (r *running) until(t *testing.T, ok func([]task.Task) bool) []task.Task {
	t.Helper()
	for {
		if s := r.next(t); ok(s) {
			return s
		}
	}
}

func (r *running) stop(t *testing.T) error {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
		return nil
	}
}

func item(id, ts string, due, created int64) map[string]any {
	it := map[string]any{"item_id": id, "item_type": "message", "state": "in_progress", "date_due": due, "date_created": created}
	if ts != "" {
		it["ts"] = ts
	}
	return it
}

func msg(ts, user, text string) map[string]any {
	return map[string]any{"type": "message", "ts": ts, "user": user, "text": text}
}

func keys(tasks []task.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Key
	}
	return out
}
