package slack

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func TestLater_LoadsEveryOpenItemAcrossPagesInOrder(t *testing.T) {
	f := newFakeSlack(t)
	f.pageSize = 3
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.channels["D1"] = map[string]any{"is_im": true, "user": "U3"}
	f.channels["G1"] = map[string]any{"is_mpim": true, "name": "mpdm-a--b"}
	f.users["U1"] = "Jane Doe"
	f.users["U2"] = "Bob Lee"
	f.users["U3"] = "Sam"
	f.history["C1/100.1"] = msg("100.1", "U1", "hi <@U2> see <https://x.test|the doc>\n   now &amp; later")
	f.replies["C1/100.2"] = map[string]any{"ts": "100.2", "username": "Deploys", "text": "a reply"}
	f.history["D1/200.1"] = msg("200.1", "U3", "dm text")
	f.history["G1/300.1"] = msg("300.1", "U1", "group text")
	f.history["C1/100.4"] = msg("100.4", "U1", "second")
	reminder := item("Sa1", "", 3000, 1700)
	reminder["item_type"] = "reminder"
	reminder["description"] = []any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "text", "text": "buy "},
			map[string]any{"type": "user", "user_id": "U2"},
			map[string]any{"type": "text", "text": " milk"},
		}},
	}}}
	completed := item("C1", "100.3", 0, 100)
	completed["state"] = "completed"
	f.setItems(
		item("C1", "100.1", 2000, 1000),
		item("C1", "100.2", 0, 500),
		item("D1", "200.1", 1000, 1500),
		item("G1", "300.1", 0, 1600),
		reminder,
		completed,
		item("C9", "900.1", 0, 1800),
		item("C1", "100.4", 0, 1900),
	)

	r := runSource(t, f.later())
	got := r.settle(t)

	wantKeys := []string{"D1/200.1", "C1/100.1", "Sa1", "C1/100.2", "G1/300.1", "C9/900.1", "C1/100.4"}
	if !reflect.DeepEqual(wantKeys, keys(got)) {
		t.Fatalf("want order %v, got %v", wantKeys, keys(got))
	}
	byKey := map[string]task.Task{}
	for _, tk := range got {
		byKey[tk.Key] = tk
	}
	assertEqual(t, "hi @Bob Lee see the doc now & later", byKey["C1/100.1"].Title)
	assertEqual(t, "#devs · Jane Doe", byKey["C1/100.1"].Detail)
	assertEqual(t, "slack://channel?id=C1&message=100.1&team=T1", byKey["C1/100.1"].URL)
	assertEqual(t, "slack-later", byKey["C1/100.1"].Source)
	if want := time.Unix(2000, 0); !want.Equal(byKey["C1/100.1"].Due) {
		t.Errorf("want due %v, got %v", want, byKey["C1/100.1"].Due)
	}
	if want := time.Unix(1000, 0); !want.Equal(byKey["C1/100.1"].Created) {
		t.Errorf("want created %v, got %v", want, byKey["C1/100.1"].Created)
	}
	assertEqual(t, "#devs · Deploys", byKey["C1/100.2"].Detail)
	if !byKey["C1/100.2"].Due.IsZero() {
		t.Errorf("want no due date, got %v", byKey["C1/100.2"].Due)
	}
	assertEqual(t, "DM with Sam", byKey["D1/200.1"].Detail)
	assertEqual(t, "group DM · Jane Doe", byKey["G1/300.1"].Detail)
	assertEqual(t, "buy @Bob Lee milk", byKey["Sa1"].Title)
	assertEqual(t, "Reminder", byKey["Sa1"].Detail)
	assertEqual(t, "slack://open?team=T1", byKey["Sa1"].URL)
	assertEqual(t, untitledMessage, byKey["C9/900.1"].Title)
	assertEqual(t, "C9", byKey["C9/900.1"].Detail)
}

func TestLater_PagesWithTheCursorFromEachResponse(t *testing.T) {
	f := newFakeSlack(t)
	f.pageSize = 2
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.setItems(item("C1", "1.1", 0, 1), item("C1", "1.2", 0, 2), item("C1", "1.3", 0, 3), item("C1", "1.4", 0, 4), item("C1", "1.5", 0, 5))

	r := runSource(t, f.later())
	got := r.settle(t)

	assertEqual(t, "5", itoa(len(got)))
	var cursors, limits []string
	for _, c := range f.callsTo("saved.list") {
		cursors = append(cursors, c.params.Get("cursor"))
		limits = append(limits, c.params.Get("limit"))
	}
	if want := []string{"", "2", "4"}; !reflect.DeepEqual(want, cursors) {
		t.Errorf("want cursors %v, got %v", want, cursors)
	}
	if want := []string{"50", "50", "50"}; !reflect.DeepEqual(want, limits) {
		t.Errorf("want limits %v, got %v", want, limits)
	}
}

func TestLater_FallsBackToHistoryThenRepliesForAMessageTheBulkCallOmits(t *testing.T) {
	f := newFakeSlack(t)
	f.omitBulk["C1/1.1"] = true
	f.omitBulk["C1/1.2"] = true
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.history["C1/1.1"] = msg("1.1", "", "top level")
	f.replies["C1/1.2"] = msg("1.2", "", "in a thread")
	f.setItems(item("C1", "1.1", 0, 1), item("C1", "1.2", 0, 2))

	r := runSource(t, f.later())
	got := r.settle(t)

	assertEqual(t, "top level", got[0].Title)
	assertEqual(t, "in a thread", got[1].Title)
	var historyTs, repliesTs []string
	for _, c := range f.callsTo("conversations.history") {
		historyTs = append(historyTs, c.params.Get("latest"))
		assertEqual(t, "1", c.params.Get("limit"))
		assertEqual(t, "true", c.params.Get("inclusive"))
		assertEqual(t, c.params.Get("latest"), c.params.Get("oldest"))
	}
	for _, c := range f.callsTo("conversations.replies") {
		repliesTs = append(repliesTs, c.params.Get("ts"))
		assertEqual(t, c.params.Get("ts"), c.params.Get("latest"))
		assertEqual(t, c.params.Get("ts"), c.params.Get("oldest"))
		assertEqual(t, "C1", c.params.Get("channel"))
	}
	if want := []string{"1.1", "1.2"}; !reflect.DeepEqual(want, historyTs) {
		t.Errorf("want history lookups %v, got %v", want, historyTs)
	}
	if want := []string{"1.2"}; !reflect.DeepEqual(want, repliesTs) {
		t.Errorf("want replies lookups %v, got %v", want, repliesTs)
	}
}

func TestLater_LooksUpEachChannelAndUserOnce(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.users["U1"] = "Jane"
	f.history["C1/1.1"] = msg("1.1", "U1", "one")
	f.history["C1/1.2"] = msg("1.2", "U1", "two")
	f.setItems(item("C1", "1.1", 0, 1), item("C1", "1.2", 0, 2))

	r := runSource(t, f.later())
	r.settle(t)

	assertEqual(t, "0", itoa(len(f.callsTo("conversations.info"))))
	assertEqual(t, "1", itoa(len(f.callsTo("users.conversations"))))
	assertEqual(t, "1", itoa(len(f.callsTo("users.info"))))
}

func TestLater_AppliesLiveEventsToTheSnapshot(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.users["U1"] = "Jane"
	f.history["C1/1.1"] = msg("1.1", "U1", "first")
	f.history["C1/1.5"] = msg("1.5", "U1", "fifth")
	f.setItems(item("C1", "1.1", 0, 1))
	r := runSource(t, f.later())
	assertEqual(t, "1", itoa(len(r.settle(t))))
	conn := f.nextConn(t)

	t.Run("saved_added adds the item", func(t *testing.T) {
		sendEvent(t, conn, map[string]any{"type": "saved_added", "saved": item("C1", "1.5", 5000, 2000)})

		got := r.settle(t)

		if want := []string{"C1/1.5", "C1/1.1"}; !reflect.DeepEqual(want, keys(got)) {
			t.Fatalf("want %v, got %v", want, keys(got))
		}
		assertEqual(t, "fifth", got[0].Title)
	})

	t.Run("saved_updated changes the due date in place and keeps what the event leaves out", func(t *testing.T) {
		updated := item("C1", "1.5", 6000, 0)
		delete(updated, "date_created")
		sendEvent(t, conn, map[string]any{"type": "saved_updated", "saved": updated})

		got := r.next(t)

		assertEqual(t, "2", itoa(len(got)))
		if want := time.Unix(6000, 0); !want.Equal(got[0].Due) {
			t.Errorf("want due %v, got %v", want, got[0].Due)
		}
		if want := time.Unix(2000, 0); !want.Equal(got[0].Created) {
			t.Errorf("want created %v, got %v", want, got[0].Created)
		}
	})

	t.Run("saved_updated to completed removes the item", func(t *testing.T) {
		done := item("C1", "1.5", 6000, 2000)
		done["state"] = "completed"
		sendEvent(t, conn, map[string]any{"type": "saved_updated", "saved": done})

		got := r.next(t)

		if want := []string{"C1/1.1"}; !reflect.DeepEqual(want, keys(got)) {
			t.Fatalf("want %v, got %v", want, keys(got))
		}
	})

	t.Run("saved_updated back to in_progress brings the item back", func(t *testing.T) {
		sendEvent(t, conn, map[string]any{"type": "saved_updated", "saved": item("C1", "1.5", 6000, 2000)})

		got := r.next(t)

		if want := []string{"C1/1.5", "C1/1.1"}; !reflect.DeepEqual(want, keys(got)) {
			t.Fatalf("want %v, got %v", want, keys(got))
		}
	})

	t.Run("saved_deleted removes the item", func(t *testing.T) {
		sendEvent(t, conn, map[string]any{"type": "saved_deleted", "saved": item("C1", "1.5", 6000, 2000)})

		got := r.next(t)

		if want := []string{"C1/1.1"}; !reflect.DeepEqual(want, keys(got)) {
			t.Fatalf("want %v, got %v", want, keys(got))
		}
	})

	t.Run("other events publish nothing", func(t *testing.T) {
		sendEvent(t, conn, map[string]any{"type": "channel_marked", "channel": "C1"})
		sendEvent(t, conn, map[string]any{"type": "message", "channel": "C1", "text": "hello"})
		sendEvent(t, conn, map[string]any{"type": "saved_deleted", "saved": item("C1", "1.1", 0, 1)})

		got := r.next(t)

		assertEqual(t, "0", itoa(len(got)))
	})

	if err := r.stop(t); err != nil {
		t.Fatalf("want nil from Run after cancel, got %v", err)
	}
}

func TestLater_ReconnectsAfterTheConnectionDropsAndReloads(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.history["C1/1.1"] = msg("1.1", "", "first")
	f.history["C1/1.2"] = msg("1.2", "", "second")
	f.setItems(item("C1", "1.1", 0, 1))
	l := f.later()
	var disconnects []error
	l.OnDisconnect = func(err error) { disconnects = append(disconnects, err) }
	r := runSource(t, l)
	assertEqual(t, "1", itoa(len(r.settle(t))))
	conn := f.nextConn(t)

	f.setItems(item("C1", "1.1", 0, 1), item("C1", "1.2", 0, 2))
	conn.Close(1001, "going away")
	got := r.settle(t)

	assertEqual(t, "2", itoa(len(got)))
	assertEqual(t, "2", itoa(len(f.callsTo("rtm.connect"))))
	assertEqual(t, "2", itoa(len(f.callsTo("saved.list"))))
	if err := r.stop(t); err != nil {
		t.Fatalf("want nil from Run after cancel, got %v", err)
	}
	if len(disconnects) != 1 || disconnects[0] == nil {
		t.Errorf("want one disconnect error, got %v", disconnects)
	}
}

func TestLater_ReconnectsWhenAPingGoesUnanswered(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.history["C1/1.1"] = msg("1.1", "", "first")
	f.setItems(item("C1", "1.1", 0, 1))
	f.setSilent(true)
	l := f.later()
	l.pingEvery = 10 * time.Millisecond
	l.pingTimeout = 30 * time.Millisecond
	var reason error
	l.OnDisconnect = func(err error) {
		if reason == nil {
			reason = err
		}
	}
	r := runSource(t, l)

	r.next(t)
	deadline := time.After(5 * time.Second)
	for len(f.callsTo("rtm.connect")) < 2 {
		select {
		case <-deadline:
			t.Fatal("want a second connection, got none")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if len(f.callsTo("rtm.connect")) < 2 {
		t.Errorf("want at least 2 connections, got %d", len(f.callsTo("rtm.connect")))
	}
	r.stop(t)
	if reason == nil || !strings.Contains(reason.Error(), "pinging event connection") {
		t.Errorf("want a ping failure, got %v", reason)
	}
}

func TestLater_RunFailsWithErrSessionExpired(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{name: "when saved.list rejects the session", method: "saved.list"},
		{name: "when rtm.connect rejects the session", method: "rtm.connect"},
		{name: "when auth.test rejects the session", method: "auth.test"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSlack(t)
			f.failWith(tc.method, "invalid_auth")
			r := runSource(t, f.later())

			select {
			case err := <-r.result:
				if !errors.Is(err, task.ErrSignedOut) {
					t.Fatalf("want ErrSignedOut, got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Run did not return")
			}
		})
	}
}

func TestLater_RunFailsWithErrSessionExpiredWhenNothingIsStored(t *testing.T) {
	l := &Later{client: &Client{http: nil, loadSess: func() (session, error) { return session{}, errNoSession }}}

	err := l.Run(context.Background(), func([]task.Task) {})

	if !errors.Is(err, task.ErrSignedOut) {
		t.Fatalf("want ErrSignedOut, got %v", err)
	}
}

func TestLater_RetriesAfterATransientLoadFailure(t *testing.T) {
	f := newFakeSlack(t)
	f.failWith("saved.list", "ratelimited")
	l := f.later()
	r := runSource(t, l)
	deadline := time.After(5 * time.Second)
	for len(f.callsTo("saved.list")) < 2 {
		select {
		case <-deadline:
			t.Fatal("want a second load attempt, got none")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if err := r.stop(t); err != nil {
		t.Fatalf("want nil from Run after cancel, got %v", err)
	}
}

func TestLater_Info(t *testing.T) {
	info := NewLater("acme").Info()
	assertEqual(t, "slack-later", info.ID)
	assertEqual(t, "Slack", info.Name)
	icon, ok := info.Icons[info.Icon]
	if !ok || len(icon.SVG) == 0 || icon.Template {
		t.Errorf("want the Slack logo in its own colours as icon %q, got %+v", info.Icon, info.Icons)
	}
}

func TestConnectEvents_ErrorsNeverCarryTheConnectionURL(t *testing.T) {
	f := newFakeSlack(t)
	c := f.client()
	c.http = f.srv.Client()
	f.srv.Config.Handler = rtmConnectTo("ws://127.0.0.1:1/ws?ticket=SECRET")

	_, err := c.connectEvents(context.Background())

	if err == nil {
		t.Fatal("want an error dialling a closed port, got none")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("want an error without the connection URL, got %q", err)
	}
}
