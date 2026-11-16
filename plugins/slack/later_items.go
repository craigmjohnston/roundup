package slack

import (
	"encoding/json"
	"net/url"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

const stateCompleted = "completed"

type savedItem struct {
	ItemID      string          `json:"item_id"`
	ItemType    string          `json:"item_type"`
	Ts          string          `json:"ts"`
	State       string          `json:"state"`
	DateDue     int64           `json:"date_due"`
	DateCreated int64           `json:"date_created"`
	Description json.RawMessage `json:"description"`
}

type savedEvent struct {
	Type string
	Item savedItem
}

func (i savedItem) key() string {
	if i.Ts == "" {
		return i.ItemID
	}
	return i.ItemID + "/" + i.Ts
}

func (i savedItem) task(teamID, title, detail string) task.Task {
	return task.Task{
		Key:     i.key(),
		Source:  laterSource,
		Title:   title,
		Detail:  detail,
		URL:     i.url(teamID),
		Due:     unixTime(i.DateDue),
		Created: unixTime(i.DateCreated),
	}
}

func (i savedItem) url(teamID string) string {
	if i.Ts == "" {
		return "slack://open?" + url.Values{"team": {teamID}}.Encode()
	}
	q := url.Values{"team": {teamID}, "id": {i.ItemID}, "message": {i.Ts}}
	return "slack://channel?" + q.Encode()
}

// parseSavedEvent reads the item out of a saved_added, saved_updated or saved_deleted event.
func parseSavedEvent(data []byte) (savedEvent, bool) {
	var env struct {
		Type  string    `json:"type"`
		Saved savedItem `json:"saved"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return savedEvent{}, false
	}
	switch env.Type {
	case "saved_added", "saved_updated", "saved_deleted":
	default:
		return savedEvent{}, false
	}
	if env.Saved.ItemID == "" {
		return savedEvent{}, false
	}
	return savedEvent{Type: env.Type, Item: env.Saved}, true
}

// applyEvent returns the items after ev, and whether the set changed; items is not modified.
func applyEvent(items []savedItem, ev savedEvent) ([]savedItem, bool) {
	switch ev.Type {
	case "saved_added", "saved_updated":
		if ev.Item.State == stateCompleted {
			return withoutItem(items, ev.Item.key()), true
		}
		return withItem(items, ev.Item), true
	case "saved_deleted":
		return withoutItem(items, ev.Item.key()), true
	}
	return items, false
}

// withItem replaces the item with the same key, keeping the fields the event leaves out.
func withItem(items []savedItem, it savedItem) []savedItem {
	out := make([]savedItem, 0, len(items)+1)
	replaced := false
	for _, old := range items {
		if old.key() != it.key() {
			out = append(out, old)
			continue
		}
		if it.DateCreated == 0 {
			it.DateCreated = old.DateCreated
		}
		if len(it.Description) == 0 {
			it.Description = old.Description
		}
		out = append(out, it)
		replaced = true
	}
	if !replaced {
		out = append(out, it)
	}
	return out
}

func withoutItem(items []savedItem, key string) []savedItem {
	out := make([]savedItem, 0, len(items))
	for _, it := range items {
		if it.key() != key {
			out = append(out, it)
		}
	}
	return out
}

func unixTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
