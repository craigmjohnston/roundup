package slack

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCache_RoundTripsWhatWasSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundup", "slack-acme.json")
	c := loadCache(path)
	c.setChannel("C1", "#devs")
	c.setUser("U1", cachedUser{Name: "Jane", Avatar: "https://img.test/u1.png", Bot: true})
	c.setItem("C1/1.1", cachedItem{Title: "hi", Author: "Jane", UserID: "U1"})

	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	got := loadCache(path)

	if !reflect.DeepEqual(c.channels, got.channels) || !reflect.DeepEqual(c.users, got.users) || !reflect.DeepEqual(c.items, got.items) {
		t.Fatalf("want %+v, got %+v", c, got)
	}
}

func TestCache_SavesPrivatelyAndLeavesNoTempFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "roundup")
	path := filepath.Join(dir, "slack-acme.json")
	c := loadCache(path)
	c.setUser("U1", cachedUser{Name: "Jane"})

	if err := c.save(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "-rw-------", file.Mode().String())
	d, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "drwx------", d.Mode().String())
	entries, _ := os.ReadDir(dir)
	assertEqual(t, "1", itoa(len(entries)))
}

func TestCache_SaveDoesNothingWhenNothingChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-acme.json")
	c := loadCache(path)

	if err := c.save(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("want no file, got err %v", err)
	}
}

func TestCache_LoadsEmptyFromAnUnusableFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		write   bool
	}{
		{name: "a missing file"},
		{name: "a corrupt file", content: "{not json", write: true},
		{name: "another version", content: `{"version":99,"users":{"U1":{"name":"Jane"}}}`, write: true},
		{name: "the previous version", content: `{"version":1,"users":{"U1":"Jane"}}`, write: true},
		{name: "no version", content: `{"users":{"U1":{"name":"Jane"}}}`, write: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "slack-acme.json")
			if tc.write {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			c := loadCache(path)

			assertEqual(t, "0", itoa(len(c.users)+len(c.channels)+len(c.items)))
			c.setUser("U2", cachedUser{Name: "Bob"})
			if err := c.save(); err != nil {
				t.Fatalf("want a usable cache, got %v", err)
			}
		})
	}
}

func TestCache_PruneDropsItemsThatAreNoLongerSaved(t *testing.T) {
	c := loadCache("")
	c.setItem("a", cachedItem{Title: "A"})
	c.setItem("b", cachedItem{Title: "B"})

	c.prune(map[string]bool{"b": true})

	if _, ok := c.items["a"]; ok || len(c.items) != 1 {
		t.Fatalf("want only b, got %v", c.items)
	}
}

func TestCache_WithNoPathNeverWrites(t *testing.T) {
	c := loadCache("")
	c.setUser("U1", cachedUser{Name: "Jane"})

	if err := c.save(); err != nil {
		t.Fatal(err)
	}
}
