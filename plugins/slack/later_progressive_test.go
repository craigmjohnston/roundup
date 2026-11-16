package slack

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func titled(tasks []task.Task) bool {
	for _, tk := range tasks {
		if tk.Title == "" {
			return false
		}
	}
	return true
}

func hasKey(tasks []task.Task, key string) bool {
	for _, tk := range tasks {
		if tk.Key == key {
			return true
		}
	}
	return false
}

func lookupCalls(f *fakeSlack) int {
	n := 0
	for _, m := range []string{"messages.list", "conversations.history", "conversations.replies", "conversations.info", "users.info", "users.conversations"} {
		n += len(f.callsTo(m))
	}
	return n
}

func seedTwoItems(f *fakeSlack) {
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.users["U1"] = "Jane"
	f.history["C1/1.1"] = msg("1.1", "U1", "first")
	f.history["C1/1.2"] = msg("1.2", "U1", "second")
	f.setItems(item("C1", "1.1", 0, 1), item("C1", "1.2", 0, 2))
}

func TestLater_PublishesTheItemListBeforeAnyMessageLookup(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	release := f.holdMessages()
	defer release()
	r := runSource(t, f.later())

	first := r.next(t)

	assertEqual(t, "2", itoa(len(first)))
	assertEqual(t, "0", itoa(len(f.callsTo("messages.list"))))
	assertEqual(t, "0", itoa(len(f.callsTo("conversations.history"))))
	for _, tk := range first {
		assertEqual(t, "", tk.Title)
	}
}

func TestLater_ChannelNamesArriveBeforeTitlesAndRowsFillIn(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	release := f.holdMessages()
	r := runSource(t, f.later())

	withChannel := r.until(t, func(s []task.Task) bool { return len(s) == 2 && s[0].Detail == "#devs" })
	release()
	final := r.settle(t)

	assertEqual(t, "", withChannel[0].Title)
	assertEqual(t, "first", final[0].Title)
	assertEqual(t, "#devs · Jane", final[0].Detail)
	assertEqual(t, "second", final[1].Title)
}

func TestLater_AppliesAnEventWhileLookupsAreInProgress(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	release := f.holdMessages()
	r := runSource(t, f.later())
	r.next(t)
	conn := f.nextConn(t)

	sendEvent(t, conn, map[string]any{"type": "saved_deleted", "saved": item("C1", "1.1", 0, 1)})
	afterEvent := r.until(t, func(s []task.Task) bool { return !hasKey(s, "C1/1.1") })
	release()
	final := r.settle(t)

	assertEqual(t, "1", itoa(len(afterEvent)))
	if want := []string{"C1/1.2"}; !reflect.DeepEqual(want, keys(final)) || !titled(final) {
		t.Fatalf("want only the second item with a title, got %+v", final)
	}
}

func TestLater_DoesNoLookupsOnAWarmStart(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	first := runSource(t, f.later())
	cold := first.settle(t)
	first.stop(t)
	before := lookupCalls(f)

	second := runSource(t, f.later())
	firstSnapshot := second.next(t)
	second.settle(t)

	if !titled(cold) || !reflect.DeepEqual(cold, firstSnapshot) {
		t.Fatalf("want the warm first snapshot to equal the cold final one, got %+v and %+v", cold, firstSnapshot)
	}
	assertEqual(t, "0", itoa(lookupCalls(f)-before))
}

func TestLater_LooksUpOnlyTheNewItemOnAReconnect(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	r := runSource(t, f.later())
	r.settle(t)
	conn := f.nextConn(t)
	f.history["C1/1.3"] = msg("1.3", "U1", "third")
	f.setItems(item("C1", "1.1", 0, 1), item("C1", "1.2", 0, 2), item("C1", "1.3", 0, 3))

	conn.Close(1001, "going away")
	final := r.settle(t)

	assertEqual(t, "3", itoa(len(final)))
	lists := f.callsTo("messages.list")
	assertEqual(t, "2", itoa(len(lists)))
	assertEqual(t, `[{"channel":"C1","timestamps":["1.3"]}]`, lists[1].params.Get("message_ids"))
	assertEqual(t, "1", itoa(len(f.callsTo("users.conversations"))))
	assertEqual(t, "1", itoa(len(f.callsTo("users.info"))))
}

func TestLater_UsesBulkLookupsAndFallsBackForWhatTheyOmit(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.channels["C2"] = map[string]any{"name": "old"}
	f.left["C2"] = true
	f.users["U1"] = "Jane"
	f.users["U2"] = "Bob"
	f.notInBulk["U2"] = true
	f.history["C1/1.1"] = msg("1.1", "U1", "one")
	f.history["C2/2.1"] = msg("2.1", "U2", "two")
	f.setItems(item("C1", "1.1", 0, 1), item("C2", "2.1", 0, 2))
	r := runSource(t, f.later())

	got := r.settle(t)

	assertEqual(t, "#old · Bob", got[1].Detail)
	assertEqual(t, "#devs · Jane", got[0].Detail)
	conv := f.callsTo("users.conversations")
	assertEqual(t, "1", itoa(len(conv)))
	assertEqual(t, "public_channel,private_channel,mpim,im", conv[0].params.Get("types"))
	assertEqual(t, "1000", conv[0].params.Get("limit"))
	assertEqual(t, "false", conv[0].params.Get("exclude_archived"))
	info := f.callsTo("conversations.info")
	assertEqual(t, "1", itoa(len(info)))
	assertEqual(t, "C2", info[0].params.Get("channel"))
	users := f.callsTo("users.info")
	assertEqual(t, "2", itoa(len(users)))
	assertEqual(t, "U1,U2", users[0].params.Get("users"))
	assertEqual(t, "U2", users[1].params.Get("user"))
}

func TestLater_FollowsTheConversationsCursor(t *testing.T) {
	f := newFakeSlack(t)
	f.convPage = 1
	f.channels["C1"] = map[string]any{"name": "a"}
	f.channels["C2"] = map[string]any{"name": "b"}
	f.history["C1/1.1"] = msg("1.1", "", "one")
	f.history["C2/2.1"] = msg("2.1", "", "two")
	f.setItems(item("C1", "1.1", 0, 1), item("C2", "2.1", 0, 2))
	r := runSource(t, f.later())

	got := r.settle(t)

	assertEqual(t, "#a", got[0].Detail)
	assertEqual(t, "#b", got[1].Detail)
	assertEqual(t, "2", itoa(len(f.callsTo("users.conversations"))))
	assertEqual(t, "0", itoa(len(f.callsTo("conversations.info"))))
}

func TestLater_BatchesUsersInGroupsOfForty(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	var items []map[string]any
	for i := range 85 {
		u, ts := fmt.Sprintf("U%03d", i), fmt.Sprintf("1.%d", i)
		f.users[u] = "Name" + u
		f.history["C1/"+ts] = msg(ts, u, "text")
		items = append(items, item("C1", ts, 0, int64(i)))
	}
	f.setItems(items...)
	r := runSource(t, f.later())

	got := r.settle(t)

	assertEqual(t, "85", itoa(len(got)))
	var sizes []int
	for _, c := range f.callsTo("users.info") {
		assertEqual(t, "", c.params.Get("user"))
		sizes = append(sizes, len(strings.Split(c.params.Get("users"), ",")))
	}
	if want := []int{40, 40, 5}; !reflect.DeepEqual(want, sizes) {
		t.Fatalf("want batches %v, got %v", want, sizes)
	}
}

func TestLater_BatchesMessagesInGroupsOfTwentyChannels(t *testing.T) {
	f := newFakeSlack(t)
	var items []map[string]any
	for i := range 45 {
		ch := fmt.Sprintf("C%02d", i)
		f.channels[ch] = map[string]any{"name": "c" + ch}
		f.history[ch+"/1.1"] = msg("1.1", "", "text "+ch)
		items = append(items, item(ch, "1.1", 0, int64(i)))
	}
	f.setItems(items...)
	r := runSource(t, f.later())

	got := r.settle(t)

	assertEqual(t, "45", itoa(len(got)))
	lists := f.callsTo("messages.list")
	assertEqual(t, "3", itoa(len(lists)))
	assertEqual(t, "0", itoa(len(f.callsTo("conversations.history"))))
}

func TestLater_ReadsThreadRepliesFromTheBulkCall(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.replies["C1/1.2"] = msg("1.2", "", "in a thread")
	f.setItems(item("C1", "1.2", 0, 1))
	r := runSource(t, f.later())

	got := r.settle(t)

	assertEqual(t, "in a thread", got[0].Title)
	assertEqual(t, "0", itoa(len(f.callsTo("conversations.history"))+len(f.callsTo("conversations.replies"))))
}

func TestLater_FallsBackToSingleLookupsWhenTheBulkCallFails(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	f.failWith("messages.list", "ratelimited")
	r := runSource(t, f.later())

	got := r.settle(t)

	assertEqual(t, "first", got[0].Title)
	assertEqual(t, "second", got[1].Title)
	assertEqual(t, "2", itoa(len(f.callsTo("conversations.history"))))
}

func TestLater_DoesNotCacheAMessageItCouldNotFind(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	f.setItems(item("C1", "1.1", 0, 1), item("C1", "9.9", 0, 2))
	r := runSource(t, f.later())

	got := r.settle(t)

	assertEqual(t, untitledMessage, got[1].Title)
	data, err := os.ReadFile(f.cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), untitledMessage) || strings.Contains(string(data), "9.9") {
		t.Errorf("want the placeholder left out of the cache, got %s", data)
	}
	r.stop(t)
	again := runSource(t, f.later())
	again.settle(t)
	assertEqual(t, "2", itoa(len(f.callsTo("messages.list"))))
}

func TestLater_WritesACacheWithoutCredentials(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	r := runSource(t, f.later())
	r.settle(t)

	data, err := os.ReadFile(f.cachePath)
	if err != nil {
		t.Fatal(err)
	}

	for _, secret := range []string{"xoxc", "xoxd"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("want no %s in the cache", secret)
		}
	}
	for _, want := range []string{`"#devs"`, `"Jane"`, `"first"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("want %s in the cache, got %s", want, data)
		}
	}
}

func TestLater_PublishesOnceAtTheEndWhenTheThrottleIsLong(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	l := f.later()
	l.publishEvery = time.Hour
	r := runSource(t, l)

	first := r.next(t)
	<-r.loaded

	assertEqual(t, "", first[0].Title)
	assertEqual(t, "1", itoa(len(r.snapshots)))
	if last := <-r.snapshots; !titled(last) {
		t.Errorf("want the last snapshot complete, got %+v", last)
	}
}

func TestLater_AResolvedReminderMentionIsCached(t *testing.T) {
	f := newFakeSlack(t)
	f.users["U2"] = "Bob"
	reminder := item("Sa1", "", 0, 1)
	reminder["description"] = []any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "user", "user_id": "U2"},
		}},
	}}}
	f.setItems(reminder)
	r := runSource(t, f.later())
	assertEqual(t, "@Bob", r.settle(t)[0].Title)
	r.stop(t)
	before := lookupCalls(f)

	again := runSource(t, f.later())
	assertEqual(t, "@Bob", again.next(t)[0].Title)
	again.settle(t)

	assertEqual(t, "0", itoa(lookupCalls(f)-before))
}

func TestLater_RunFailsWithErrSessionExpiredDuringLookups(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	f.failWith("messages.list", "invalid_auth")
	r := runSource(t, f.later())

	for {
		select {
		case <-r.snapshots:
		case err := <-r.result:
			if err == nil || !strings.Contains(err.Error(), "signed out") {
				t.Fatalf("want ErrSignedOut, got %v", err)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return")
		}
	}
}
