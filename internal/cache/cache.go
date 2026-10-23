// Package cache is a small persistent JSON key/value store for API responses,
// so repeated scans of the same pack need no network round trips.
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type entry struct {
	Stored time.Time       `json:"t"`
	Value  json.RawMessage `json:"v"` // "null" records a confirmed miss
}

// Cache is safe for concurrent use. A nil *Cache is a valid no-op cache.
type Cache struct {
	path    string
	ttl     time.Duration
	missTTL time.Duration
	mu      sync.Mutex
	entries map[string]entry
	dirty   bool
}

// DefaultPath is the cache file under the user's cache directory.
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcmodfilter", "cache.json"), nil
}

// Open loads the cache at path; a missing or corrupt file starts empty.
func Open(path string, ttl time.Duration) *Cache {
	c := &Cache{path: path, ttl: ttl, missTTL: ttl / 7, entries: map[string]entry{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &c.entries)
	}
	return c
}

// Get decodes the cached value for key into v. found reports a fresh entry;
// hit reports that the entry holds a value rather than a recorded miss.
func (c *Cache) Get(key string, v any) (found, hit bool) {
	if c == nil {
		return false, false
	}
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if !ok {
		return false, false
	}
	miss := string(e.Value) == "null"
	ttl := c.ttl
	if miss {
		ttl = c.missTTL
	}
	if time.Since(e.Stored) > ttl {
		return false, false
	}
	if miss {
		return true, false
	}
	return json.Unmarshal(e.Value, v) == nil, true
}

// Put stores v under key; a nil v records a miss.
func (c *Cache) Put(key string, v any) {
	if c == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.entries[key] = entry{Stored: time.Now(), Value: raw}
	c.dirty = true
	c.mu.Unlock()
}

// Save writes the cache to disk if it changed.
func (c *Cache) Save() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	for k, e := range c.entries {
		if time.Since(e.Stored) > c.ttl {
			delete(c.entries, k)
		}
	}
	data, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), "cache-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	c.dirty = false
	return os.Rename(tmp.Name(), c.path)
}
