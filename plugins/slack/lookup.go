package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	userBatch       = 40
	messageChannels = 20
)

// resolvedItem is the display text for one saved item; final is false while a name it uses is still unknown.
type resolvedItem struct {
	title  string
	author string
	userID string
	final  bool
}

type lookupRequest struct {
	items    []savedItem
	channels []string
	users    map[string]cachedUser
}

// lookupUpdate is what one step of runLookups learned; unresolved lists channels that could not be looked up.
type lookupUpdate struct {
	channels   map[string]string
	unresolved []string
	users      map[string]cachedUser
	items      map[string]resolvedItem
}

type conversation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IsIM   bool   `json:"is_im"`
	IsMPIM bool   `json:"is_mpim"`
	User   string `json:"user"`
}

type slackUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsBot   bool   `json:"is_bot"`
	Profile struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Image72     string `json:"image_72"`
		Image48     string `json:"image_48"`
		Image192    string `json:"image_192"`
	} `json:"profile"`
}

type fetched struct {
	item savedItem
	msg  message
}

type lookups struct {
	client *Client
	send   func(lookupUpdate)
	users  map[string]cachedUser
	dmUser map[string]string
}

// runLookups resolves channels, then message text, then user names, sending each step's result to send.
// It returns an error only when the session has expired or ctx has ended.
func runLookups(ctx context.Context, c *Client, req lookupRequest, send func(lookupUpdate)) error {
	w := &lookups{client: c, send: send, users: req.users, dmUser: map[string]string{}}
	if err := w.resolveChannels(ctx, req.channels); err != nil {
		return err
	}
	got, err := w.fetchMessages(ctx, req.items)
	if err != nil {
		return err
	}
	return w.resolveUsers(ctx, got)
}

func (w *lookups) resolveChannels(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	labels := map[string]string{}
	listed := map[string]bool{}
	all, err := w.listConversations(ctx)
	if err != nil && fatal(ctx, err) {
		return err
	}
	for _, ch := range all {
		listed[ch.ID] = true
		w.note(ch, want[ch.ID], labels)
	}
	var unresolved []string
	for _, id := range ids {
		if listed[id] {
			continue
		}
		ch, err := w.conversationInfo(ctx, id)
		if err != nil {
			if fatal(ctx, err) {
				return err
			}
			unresolved = append(unresolved, id)
			continue
		}
		w.note(ch, true, labels)
	}
	w.send(lookupUpdate{channels: labels, unresolved: unresolved})
	return nil
}

// note records a channel's label, or for a DM the other person, whose name is not known until users are looked up.
func (w *lookups) note(ch conversation, wanted bool, labels map[string]string) {
	switch {
	case ch.IsIM:
		if wanted {
			w.dmUser[ch.ID] = ch.User
		}
	case ch.IsMPIM:
		labels[ch.ID] = "group DM"
	default:
		labels[ch.ID] = "#" + ch.Name
	}
}

func (w *lookups) listConversations(ctx context.Context) ([]conversation, error) {
	var all []conversation
	cursor := ""
	for {
		params := url.Values{
			"types":            {"public_channel,private_channel,mpim,im"},
			"limit":            {"1000"},
			"exclude_archived": {"false"},
		}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		var page struct {
			Channels         []conversation `json:"channels"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := w.call(ctx, "users.conversations", params, &page); err != nil {
			return all, err
		}
		all = append(all, page.Channels...)
		cursor = page.ResponseMetadata.NextCursor
		if cursor == "" {
			return all, nil
		}
	}
}

func (w *lookups) conversationInfo(ctx context.Context, id string) (conversation, error) {
	var out struct {
		Channel conversation `json:"channel"`
	}
	if err := w.call(ctx, "conversations.info", url.Values{"channel": {id}}, &out); err != nil {
		return conversation{}, err
	}
	out.Channel.ID = id
	return out.Channel, nil
}

// fetchMessages looks messages up in bulk, 20 channels per call, and singly for any the bulk call leaves out.
// It sends a provisional title for each batch as it arrives.
func (w *lookups) fetchMessages(ctx context.Context, items []savedItem) ([]fetched, error) {
	var got []fetched
	provisional := lookupUpdate{items: map[string]resolvedItem{}}
	var messages []savedItem
	for _, it := range items {
		if it.Ts != "" {
			messages = append(messages, it)
			continue
		}
		got = append(got, fetched{item: it})
		provisional.items[it.key()], _ = w.build(fetched{item: it}, false)
	}
	if len(provisional.items) > 0 {
		w.send(provisional)
	}

	for _, batch := range messageBatches(messages) {
		bulk, err := w.bulkMessages(ctx, batch)
		if err != nil {
			return nil, err
		}
		up := lookupUpdate{items: map[string]resolvedItem{}}
		for _, it := range batch {
			msg, found := bulk[it.key()]
			if !found {
				var err error
				if msg, found, err = w.findMessage(ctx, it.ItemID, it.Ts); err != nil {
					return nil, err
				}
			}
			if !found {
				up.items[it.key()] = resolvedItem{title: untitledMessage}
				continue
			}
			f := fetched{item: it, msg: msg}
			got = append(got, f)
			up.items[it.key()], _ = w.build(f, false)
		}
		w.send(up)
	}
	return got, nil
}

// messageBatches splits items into groups that span at most messageChannels channels.
func messageBatches(items []savedItem) [][]savedItem {
	var batches [][]savedItem
	var batch []savedItem
	channels := map[string]bool{}
	for _, it := range items {
		if !channels[it.ItemID] && len(channels) == messageChannels {
			batches = append(batches, batch)
			batch, channels = nil, map[string]bool{}
		}
		channels[it.ItemID] = true
		batch = append(batch, it)
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

// bulkMessages returns the messages messages.list has for the batch, keyed like savedItem.key; a failed call returns none.
func (w *lookups) bulkMessages(ctx context.Context, batch []savedItem) (map[string]message, error) {
	type entry struct {
		Channel    string   `json:"channel"`
		Timestamps []string `json:"timestamps"`
	}
	var entries []*entry
	byChannel := map[string]*entry{}
	for _, it := range batch {
		e, ok := byChannel[it.ItemID]
		if !ok {
			e = &entry{Channel: it.ItemID}
			byChannel[it.ItemID] = e
			entries = append(entries, e)
		}
		e.Timestamps = append(e.Timestamps, it.Ts)
	}
	ids, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("encoding message ids: %w", err)
	}
	var out struct {
		Messages map[string][]message `json:"messages"`
	}
	if err := w.call(ctx, "messages.list", url.Values{"message_ids": {string(ids)}}, &out); err != nil {
		if fatal(ctx, err) {
			return nil, err
		}
		return nil, nil
	}
	found := map[string]message{}
	for channel, msgs := range out.Messages {
		for _, m := range msgs {
			found[channel+"/"+m.Ts] = m
		}
	}
	return found, nil
}

// findMessage looks for ts in the channel history, then in the thread it replies to.
func (w *lookups) findMessage(ctx context.Context, channel, ts string) (message, bool, error) {
	for _, method := range []string{"conversations.history", "conversations.replies"} {
		params := url.Values{
			"channel":   {channel},
			"latest":    {ts},
			"oldest":    {ts},
			"inclusive": {"true"},
			"limit":     {"1"},
		}
		if method == "conversations.replies" {
			params.Set("ts", ts)
		}
		var out struct {
			Messages []message `json:"messages"`
		}
		if err := w.call(ctx, method, params, &out); err != nil {
			if fatal(ctx, err) {
				return message{}, false, err
			}
			return message{}, false, nil
		}
		for _, m := range out.Messages {
			if m.Ts == ts {
				return m, true, nil
			}
		}
	}
	return message{}, false, nil
}

func (w *lookups) resolveUsers(ctx context.Context, got []fetched) error {
	seen := map[string]bool{}
	var need []string
	add := func(id string) {
		if _, known := w.users[id]; !known && !seen[id] {
			seen[id] = true
			need = append(need, id)
		}
	}
	for _, f := range got {
		_, missing := w.build(f, false)
		for _, id := range missing {
			add(id)
		}
	}
	for _, id := range w.dmUser {
		add(id)
	}
	sort.Strings(need)
	found, err := w.lookupUsers(ctx, need)
	if err != nil {
		return err
	}

	up := lookupUpdate{users: found, channels: map[string]string{}, items: map[string]resolvedItem{}}
	for ch, id := range w.dmUser {
		if u, ok := w.users[id]; ok {
			up.channels[ch] = "DM with " + u.Name
		} else {
			up.unresolved = append(up.unresolved, ch)
		}
	}
	for _, f := range got {
		up.items[f.item.key()], _ = w.build(f, true)
	}
	w.send(up)
	return nil
}

// lookupUsers fetches names in batches and falls back to one call per id the batch left out.
func (w *lookups) lookupUsers(ctx context.Context, ids []string) (map[string]cachedUser, error) {
	found := map[string]cachedUser{}
	for len(ids) > 0 {
		batch := ids[:min(userBatch, len(ids))]
		ids = ids[len(batch):]
		var out struct {
			Users []slackUser `json:"users"`
		}
		err := w.call(ctx, "users.info", url.Values{"users": {strings.Join(batch, ",")}}, &out)
		if err != nil && fatal(ctx, err) {
			return nil, err
		}
		for _, u := range out.Users {
			if u.ID != "" {
				found[u.ID] = u.cached()
				w.users[u.ID] = u.cached()
			}
		}
		for _, id := range batch {
			if _, ok := found[id]; ok {
				continue
			}
			var one struct {
				User slackUser `json:"user"`
			}
			if err := w.call(ctx, "users.info", url.Values{"user": {id}}, &one); err != nil {
				if fatal(ctx, err) {
					return nil, err
				}
				continue
			}
			one.User.ID = id
			found[id] = one.User.cached()
			w.users[id] = one.User.cached()
		}
	}
	return found, nil
}

// build makes the display text from what is known; missing lists the user ids it needed and did not have.
// Until settled, an unknown author is left blank instead of shown as an id.
func (w *lookups) build(f fetched, settled bool) (r resolvedItem, missing []string) {
	name := func(id string) string {
		if u, ok := w.users[id]; ok {
			return u.Name
		}
		missing = append(missing, id)
		return id
	}
	if f.item.Ts == "" {
		r.title = untitledSaved
		if text := richTextOf(f.item.Description, name); text != "" {
			r.title = cleanSpace(text)
		}
	} else {
		r.title = cleanText(messageText(f.msg), name)
		if r.title == "" {
			r.title = untitledMessage
		}
		r.userID = f.msg.humanAuthor()
		switch {
		case f.msg.User == "":
			r.author = firstNonEmpty(f.msg.Username, f.msg.BotProfile.Name)
		case settled:
			r.author = name(f.msg.User)
		default:
			if u, ok := w.users[f.msg.User]; ok {
				r.author = u.Name
			} else {
				missing = append(missing, f.msg.User)
			}
		}
	}
	r.final = len(missing) == 0
	return r, missing
}

func (w *lookups) call(ctx context.Context, method string, params url.Values, out any) error {
	body, err := w.client.Call(ctx, method, params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding %s: %w", method, err)
	}
	return nil
}

func (u slackUser) displayName() string {
	return firstNonEmpty(u.Profile.DisplayName, u.Profile.RealName, u.Name, u.ID)
}

// cached is the part of a user the source keeps: a name, a photo URL and whether it is a bot.
func (u slackUser) cached() cachedUser {
	return cachedUser{
		Name:   u.displayName(),
		Avatar: firstNonEmpty(u.Profile.Image72, u.Profile.Image48, u.Profile.Image192),
		Bot:    u.IsBot,
	}
}
