package tests

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/relaydev/relay/internal/cache"
)

func setupCache(t *testing.T) *cache.Cache {
	t.Helper()
	dir, err := os.MkdirTemp("", "relay-cache-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	c := cache.New(db)
	if err := c.Init(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	return c
}

func TestCacheKey(t *testing.T) {
	k1, err := cache.Key("local", "retrieval.search", map[string]string{"query": "hello"}, "thread-1", "v1")
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if k1 == "" {
		t.Error("key should not be empty")
	}

	// Same args should produce same key
	k2, _ := cache.Key("local", "retrieval.search", map[string]string{"query": "hello"}, "thread-1", "v1")
	if k1 != k2 {
		t.Errorf("same args should produce same key: %s != %s", k1, k2)
	}

	// Different args should produce different key
	k3, _ := cache.Key("local", "retrieval.search", map[string]string{"query": "world"}, "thread-1", "v1")
	if k1 == k3 {
		t.Error("different args should produce different keys")
	}
}

func TestCacheSetGet(t *testing.T) {
	c := setupCache(t)

	preview := json.RawMessage(`{"result":"found","count":3}`)
	key := "test-key-001"

	if err := c.Set(key, "retrieval.search", key, preview, "art-ref-1", "thread-1", time.Hour); err != nil {
		t.Fatalf("set: %v", err)
	}

	entry, hit, err := c.Get(key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !hit {
		t.Error("expected cache hit")
	}
	if entry.ArtifactRef != "art-ref-1" {
		t.Errorf("expected artifact_ref art-ref-1, got %s", entry.ArtifactRef)
	}
	if string(entry.Preview) != string(preview) {
		t.Errorf("preview mismatch: got %s", entry.Preview)
	}
}

func TestCacheMiss(t *testing.T) {
	c := setupCache(t)

	_, hit, err := c.Get("nonexistent-key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if hit {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := setupCache(t)

	preview := json.RawMessage(`{"test":"data"}`)
	key := "expiry-test-key"

	// Set with very short TTL
	if err := c.Set(key, "test.cap", key, preview, "ref-1", "thread-1", 1*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	_, hit, err := c.Get(key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if hit {
		t.Error("expected cache miss after expiry")
	}
}

func TestCacheHitCount(t *testing.T) {
	c := setupCache(t)

	preview := json.RawMessage(`{}`)
	key := "hit-count-key"
	c.Set(key, "cap", key, preview, "ref", "thread", time.Hour)

	for i := 0; i < 3; i++ {
		entry, hit, _ := c.Get(key)
		if !hit {
			t.Fatalf("iteration %d: expected hit", i)
		}
		if entry.HitCount != i+1 {
			t.Errorf("iteration %d: expected hit count %d, got %d", i, i+1, entry.HitCount)
		}
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := setupCache(t)

	key := "invalidate-key"
	c.Set(key, "cap", key, json.RawMessage(`{}`), "ref", "thread", time.Hour)

	if err := c.Invalidate(key); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	_, hit, _ := c.Get(key)
	if hit {
		t.Error("expected cache miss after invalidation")
	}
}
