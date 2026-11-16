package slack

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func TestApplyEvent(t *testing.T) {
	a := savedItem{ItemID: "C1", Ts: "1.1", State: "in_progress", DateDue: 10, DateCreated: 1}
	b := savedItem{ItemID: "C1", Ts: "1.2", State: "in_progress", DateCreated: 2}

	t.Run("saved_added adds an item", func(t *testing.T) {
		got, changed := applyEvent([]savedItem{a}, savedEvent{Type: "saved_added", Item: b})

		if !changed || !reflect.DeepEqual([]savedItem{a, b}, got) {
			t.Fatalf("want %v changed, got %v changed=%v", []savedItem{a, b}, got, changed)
		}
	})

	t.Run("saved_updated replaces the item and keeps the creation date the event omits", func(t *testing.T) {
		got, changed := applyEvent([]savedItem{a, b}, savedEvent{Type: "saved_updated", Item: savedItem{ItemID: "C1", Ts: "1.1", State: "in_progress", DateDue: 99}})

		want := []savedItem{{ItemID: "C1", Ts: "1.1", State: "in_progress", DateDue: 99, DateCreated: 1}, b}
		if !changed || !reflect.DeepEqual(want, got) {
			t.Fatalf("want %v changed, got %v changed=%v", want, got, changed)
		}
	})

	t.Run("saved_updated to completed removes the item", func(t *testing.T) {
		got, changed := applyEvent([]savedItem{a, b}, savedEvent{Type: "saved_updated", Item: savedItem{ItemID: "C1", Ts: "1.1", State: "completed"}})

		if !changed || !reflect.DeepEqual([]savedItem{b}, got) {
			t.Fatalf("want %v changed, got %v changed=%v", []savedItem{b}, got, changed)
		}
	})

	t.Run("saved_updated can bring a removed item back", func(t *testing.T) {
		got, changed := applyEvent([]savedItem{b}, savedEvent{Type: "saved_updated", Item: a})

		if !changed || !reflect.DeepEqual([]savedItem{b, a}, got) {
			t.Fatalf("want %v changed, got %v changed=%v", []savedItem{b, a}, got, changed)
		}
	})

	t.Run("saved_deleted removes the item", func(t *testing.T) {
		got, changed := applyEvent([]savedItem{a, b}, savedEvent{Type: "saved_deleted", Item: a})

		if !changed || !reflect.DeepEqual([]savedItem{b}, got) {
			t.Fatalf("want %v changed, got %v changed=%v", []savedItem{b}, got, changed)
		}
	})

	t.Run("deleting an item that is not there changes nothing", func(t *testing.T) {
		got, _ := applyEvent([]savedItem{a}, savedEvent{Type: "saved_deleted", Item: b})

		if !reflect.DeepEqual([]savedItem{a}, got) {
			t.Fatalf("want %v, got %v", []savedItem{a}, got)
		}
	})

	t.Run("an unknown event leaves the items alone", func(t *testing.T) {
		got, changed := applyEvent([]savedItem{a}, savedEvent{Type: "channel_marked", Item: b})

		if changed || !reflect.DeepEqual([]savedItem{a}, got) {
			t.Fatalf("want %v unchanged, got %v changed=%v", []savedItem{a}, got, changed)
		}
	})

	t.Run("the input slice is not modified", func(t *testing.T) {
		in := []savedItem{a, b}

		applyEvent(in, savedEvent{Type: "saved_deleted", Item: a})

		if !reflect.DeepEqual([]savedItem{a, b}, in) {
			t.Fatalf("want input untouched, got %v", in)
		}
	})
}

func TestParseSavedEvent(t *testing.T) {
	t.Run("reads a message item as Slack sends it", func(t *testing.T) {
		ev, ok := parseSavedEvent([]byte(`{"type":"saved_updated","saved":{"item_id":"C1","item_type":"message","date_created":1794309369,"date_due":0,"date_completed":1794406877,"date_updated":1794406877,"is_archived":false,"date_snoozed_until":0,"ts":"1794307486.928869","state":"completed","todo_state":"completed"},"counts":{"uncompleted_count":13},"event_ts":"1794406877.011200"}`))

		want := savedEvent{Type: "saved_updated", Item: savedItem{ItemID: "C1", ItemType: "message", Ts: "1794307486.928869", State: "completed", DateCreated: 1794309369}}
		if !ok || !reflect.DeepEqual(want, ev) {
			t.Fatalf("want %v, got %v ok=%v", want, ev, ok)
		}
	})

	t.Run("reads a reminder item, which has a description and no ts", func(t *testing.T) {
		ev, ok := parseSavedEvent([]byte(`{"type":"saved_added","saved":{"item_id":"Sa1","item_type":"reminder","date_created":1794406943,"date_due":1794407003,"description":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"test"}]}]}],"state":"in_progress","todo_state":"to_do"},"event_ts":"1794406943.011800"}`))

		if !ok {
			t.Fatal("want the event parsed, got ok=false")
		}
		if ev.Item.ItemID != "Sa1" || ev.Item.Ts != "" || ev.Item.DateDue != 1794407003 || len(ev.Item.Description) == 0 {
			t.Fatalf("got %+v", ev.Item)
		}
	})

	tests := []struct {
		name string
		data string
	}{
		{name: "an unrelated event", data: `{"type":"channel_marked","channel":"C1"}`},
		{name: "saved_due, which changes nothing in the set", data: `{"type":"saved_due","saved":{"item_id":"C1","ts":"1.1"}}`},
		{name: "a saved event with no item id", data: `{"type":"saved_added","saved":{}}`},
		{name: "something that is not JSON", data: `nope`},
	}
	for _, tc := range tests {
		t.Run("ignores "+tc.name, func(t *testing.T) {
			if _, ok := parseSavedEvent([]byte(tc.data)); ok {
				t.Fatal("want the event ignored, got it parsed")
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	names := map[string]string{"U1": "Jane Doe", "U2": "Bob"}
	lookup := func(id string) string { return names[id] }

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "user mentions become @Name", in: "ping <@U1> and <@U2>", want: "ping @Jane Doe and @Bob"},
		{name: "a mention with a label still uses the lookup", in: "<@U1|jane> hi", want: "@Jane Doe hi"},
		{name: "links with a label keep the label", in: "see <https://x.test/a?b=1|the doc>", want: "see the doc"},
		{name: "links without a label keep the URL", in: "see <https://x.test/a>", want: "see https://x.test/a"},
		{name: "channel links become #name", in: "in <#C1|devs> and <#C2>", want: "in #devs and #C2"},
		{name: "broadcasts become @here", in: "<!here> and <!channel>", want: "@here and @channel"},
		{name: "user groups use their label", in: "<!subteam^S1|@oncall> look", want: "@oncall look"},
		{name: "whitespace collapses", in: "  one\n\n two\t three  ", want: "one two three"},
		{name: "entities are decoded", in: "a &lt; b &amp;&amp; c &gt; d", want: "a < b && c > d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertEqual(t, tc.want, cleanText(tc.in, lookup))
		})
	}
}

func TestMessageText(t *testing.T) {
	t.Run("falls back to an attachment", func(t *testing.T) {
		var m message
		json.Unmarshal([]byte(`{"attachments":[{"fallback":"build failed"}]}`), &m)

		assertEqual(t, "build failed", messageText(m))
	})

	t.Run("falls back to a file name", func(t *testing.T) {
		var m message
		json.Unmarshal([]byte(`{"files":[{"name":"report.pdf"}]}`), &m)

		assertEqual(t, "File: report.pdf", messageText(m))
	})

	t.Run("is empty when there is nothing to show", func(t *testing.T) {
		assertEqual(t, "", messageText(message{}))
	})
}

func TestSortTasks(t *testing.T) {
	at := func(sec int64) time.Time { return time.Unix(sec, 0) }
	tasks := []task.Task{
		{Key: "no-due-new", Created: at(30)},
		{Key: "due-late", Due: at(200), Created: at(1)},
		{Key: "no-due-old", Created: at(10)},
		{Key: "due-early-b", Due: at(100), Created: at(20)},
		{Key: "due-early-a", Due: at(100), Created: at(5)},
		{Key: "same-b", Created: at(40)},
		{Key: "same-a", Created: at(40)},
	}

	sortTasks(tasks)

	want := []string{"due-early-a", "due-early-b", "due-late", "no-due-old", "no-due-new", "same-a", "same-b"}
	if !reflect.DeepEqual(want, keys(tasks)) {
		t.Fatalf("want %v, got %v", want, keys(tasks))
	}
}

func TestRichTextOf(t *testing.T) {
	names := func(id string) string { return "N" + id }

	t.Run("joins text, links, users and emoji", func(t *testing.T) {
		raw := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
			{"type":"text","text":"call "},{"type":"user","user_id":"U1"},{"type":"text","text":" "},
			{"type":"link","url":"https://x.test"},{"type":"emoji","name":"tada"}]}]}]`)

		assertEqual(t, "call @NU1 https://x.test:tada:", richTextOf(raw, names))
	})

	t.Run("is empty for missing or malformed input", func(t *testing.T) {
		assertEqual(t, "", richTextOf(nil, names))
		assertEqual(t, "", richTextOf(json.RawMessage(`{`), names))
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func rtmConnectTo(wsURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true,"url":"` + wsURL + `"}`))
	})
}
