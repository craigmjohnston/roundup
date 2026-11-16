package slack

import (
	"maps"
	"strings"

	"github.com/craigmjohnston/roundup/task"
)

// view is the item set plus everything known about it; the loop goroutine is its only user.
type view struct {
	teamID     string
	items      []savedItem
	cache      *cache
	shown      map[string]resolvedItem
	unresolved map[string]bool
	attempted  map[string]bool
	requested  []string
}

func newView(teamID string, items []savedItem, c *cache) *view {
	keep := make(map[string]bool, len(items))
	for _, it := range items {
		keep[it.key()] = true
	}
	c.prune(keep)
	return &view{
		teamID:     teamID,
		items:      items,
		cache:      c,
		shown:      map[string]resolvedItem{},
		unresolved: map[string]bool{},
		attempted:  map[string]bool{},
	}
}

func (v *view) tasks() []task.Task {
	tasks := make([]task.Task, 0, len(v.items))
	for _, it := range v.items {
		title, detail, userID := v.display(it)
		tk := it.task(v.teamID, title, detail)
		if u, ok := v.cache.users[userID]; ok && userID != "" && !u.Bot {
			tk.Person, tk.AvatarURL = u.Name, u.Avatar
		}
		tasks = append(tasks, tk)
	}
	sortTasks(tasks)
	return tasks
}

// display gives an empty title until the item's text is known, and a detail with whatever parts are known.
// userID is the human author's id, empty for reminders and for messages from bots and apps.
func (v *view) display(it savedItem) (title, detail, userID string) {
	var author string
	if c, ok := v.cache.items[it.key()]; ok {
		title, author, userID = c.Title, c.Author, c.UserID
	} else if r, ok := v.shown[it.key()]; ok {
		title, author, userID = r.title, r.author, r.userID
	}
	if it.Ts == "" {
		return title, "Reminder", ""
	}
	channel := v.channelText(it.ItemID)
	switch {
	case channel == "":
		return title, author, userID
	case author == "" || strings.HasSuffix(channel, author):
		return title, channel, userID
	}
	return title, channel + " · " + author, userID
}

func (v *view) channelText(id string) string {
	if label, ok := v.cache.channels[id]; ok {
		return label
	}
	if v.unresolved[id] {
		return id
	}
	return ""
}

// request returns the lookups still to do and marks them as handed out.
func (v *view) request() (lookupRequest, bool) {
	var req lookupRequest
	seen := map[string]bool{}
	for _, it := range v.items {
		if _, ok := v.cache.items[it.key()]; !ok && !v.attempted[it.key()] {
			v.attempted[it.key()] = true
			req.items = append(req.items, it)
		}
		if it.Ts == "" || seen[it.ItemID] {
			continue
		}
		seen[it.ItemID] = true
		if _, ok := v.cache.channels[it.ItemID]; !ok && !v.unresolved[it.ItemID] {
			req.channels = append(req.channels, it.ItemID)
		}
	}
	if len(req.items) == 0 && len(req.channels) == 0 {
		return req, false
	}
	v.requested = req.channels
	req.users = maps.Clone(v.cache.users)
	return req, true
}

// merge takes in what a lookup step learned; results for items that have since gone are dropped.
func (v *view) merge(u lookupUpdate) {
	for id, label := range u.channels {
		v.cache.setChannel(id, label)
	}
	for _, id := range u.unresolved {
		v.unresolved[id] = true
	}
	for id, user := range u.users {
		v.cache.setUser(id, user)
	}
	present := make(map[string]bool, len(v.items))
	for _, it := range v.items {
		present[it.key()] = true
	}
	for key, r := range u.items {
		switch {
		case !present[key]:
		case r.final:
			v.cache.setItem(key, cachedItem{Title: r.title, Author: r.author, UserID: r.userID})
			delete(v.shown, key)
		default:
			v.shown[key] = r
		}
	}
}

// finish marks every channel the last lookup could not resolve, so it is not asked for again.
func (v *view) finish() {
	for _, id := range v.requested {
		if _, ok := v.cache.channels[id]; !ok {
			v.unresolved[id] = true
		}
	}
	v.requested = nil
}
