package cache

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c", "cache.json")
	c := Open(path, time.Hour)
	c.Put("hit", map[string]int{"n": 1})
	c.Put("miss", nil)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	c = Open(path, time.Hour)
	var v map[string]int
	if found, hit := c.Get("hit", &v); !found || !hit || v["n"] != 1 {
		t.Errorf("hit: found=%v hit=%v v=%v", found, hit, v)
	}
	if found, hit := c.Get("miss", &v); !found || hit {
		t.Errorf("miss: found=%v hit=%v", found, hit)
	}
	if found, _ := c.Get("absent", &v); found {
		t.Error("absent key found")
	}

	expired := Open(path, time.Nanosecond)
	time.Sleep(time.Millisecond)
	if found, _ := expired.Get("hit", &v); found {
		t.Error("expired entry returned")
	}

	var nilCache *Cache
	nilCache.Put("x", 1)
	if found, _ := nilCache.Get("x", &v); found || nilCache.Save() != nil {
		t.Error("nil cache should be a no-op")
	}
}
