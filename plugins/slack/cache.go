package slack

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const cacheVersion = 2

type cachedItem struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	UserID string `json:"user_id,omitempty"`
}

type cachedUser struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
	Bot    bool   `json:"bot,omitempty"`
}

type cacheFile struct {
	Version  int                   `json:"version"`
	Channels map[string]string     `json:"channels"`
	Users    map[string]cachedUser `json:"users"`
	Items    map[string]cachedItem `json:"items"`
}

// cache holds the lookups that do not change, so a restart only looks up what is new.
type cache struct {
	path     string
	channels map[string]string
	users    map[string]cachedUser
	items    map[string]cachedItem
	dirty    bool
}

// defaultCachePath is empty when the user cache directory is unknown, which turns the disk cache off.
func defaultCachePath(ws Workspace) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "roundup", "slack-"+string(ws)+".json")
}

// loadCache returns an empty cache when the file is missing, unreadable or from another version.
func loadCache(path string) *cache {
	c := &cache{
		path:     path,
		channels: map[string]string{},
		users:    map[string]cachedUser{},
		items:    map[string]cachedItem{},
	}
	if path == "" {
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var f cacheFile
	if json.Unmarshal(data, &f) != nil || f.Version != cacheVersion {
		return c
	}
	if f.Channels != nil {
		c.channels = f.Channels
	}
	if f.Users != nil {
		c.users = f.Users
	}
	if f.Items != nil {
		c.items = f.Items
	}
	return c
}

func (c *cache) setChannel(id, label string) {
	if c.channels[id] != label {
		c.channels[id] = label
		c.dirty = true
	}
}

func (c *cache) setUser(id string, u cachedUser) {
	if c.users[id] != u {
		c.users[id] = u
		c.dirty = true
	}
}

func (c *cache) setItem(key string, it cachedItem) {
	if got, ok := c.items[key]; !ok || got != it {
		c.items[key] = it
		c.dirty = true
	}
}

// prune drops the cached items whose key is not in keep.
func (c *cache) prune(keep map[string]bool) {
	for key := range c.items {
		if !keep[key] {
			delete(c.items, key)
			c.dirty = true
		}
	}
}

// save writes the cache through a temporary file and a rename, and does nothing when nothing changed.
func (c *cache) save() error {
	if c.path == "" || !c.dirty {
		return nil
	}
	data, err := json.Marshal(cacheFile{Version: cacheVersion, Channels: c.channels, Users: c.users, Items: c.items})
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "slack-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), c.path); err != nil {
		return err
	}
	c.dirty = false
	return nil
}
