package cache

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const DefaultTTL = 24 * time.Hour

// Entry represents a cached result.
type Entry struct {
	Key         string          `json:"key"`
	Capability  string          `json:"capability"`
	ArgsHash    string          `json:"args_hash"`
	Preview     json.RawMessage `json:"preview"`
	ArtifactRef string          `json:"artifact_ref"`
	ThreadID    string          `json:"thread_id"`
	CreatedAt   time.Time       `json:"created_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
	HitCount    int             `json:"hit_count"`
}

// Cache is an embedded SQLite-backed cache.
type Cache struct {
	db *sql.DB
}

func New(db *sql.DB) *Cache {
	return &Cache{db: db}
}

func (c *Cache) Init() error {
	_, err := c.db.Exec(`
		CREATE TABLE IF NOT EXISTS cache (
			key          TEXT PRIMARY KEY,
			capability   TEXT NOT NULL,
			args_hash    TEXT NOT NULL,
			preview      TEXT NOT NULL,
			artifact_ref TEXT NOT NULL,
			thread_id    TEXT NOT NULL,
			created_at   TEXT NOT NULL,
			expires_at   TEXT NOT NULL,
			hit_count    INTEGER DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_cache_capability ON cache(capability);
		CREATE INDEX IF NOT EXISTS idx_cache_expires ON cache(expires_at);
	`)
	return err
}

// Key computes the canonical cache key.
func Key(tenant, capability string, args any, scope, version string) (string, error) {
	argsJSON, err := json.Marshal(normalizeArgs(args))
	if err != nil {
		return "", fmt.Errorf("marshal args: %w", err)
	}

	h := sha256.New()
	h.Write([]byte(tenant + "|" + capability + "|" + string(argsJSON) + "|" + scope + "|" + version))
	return hex.EncodeToString(h.Sum(nil))[:32], nil
}

// Get retrieves a cache entry if valid.
func (c *Cache) Get(key string) (*Entry, bool, error) {
	var e Entry
	var createdAt, expiresAt, preview string

	err := c.db.QueryRow(`
		SELECT key, capability, args_hash, preview, artifact_ref, thread_id, created_at, expires_at, hit_count
		FROM cache WHERE key = ?
	`, key).Scan(
		&e.Key, &e.Capability, &e.ArgsHash, &preview, &e.ArtifactRef,
		&e.ThreadID, &createdAt, &expiresAt, &e.HitCount,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("query cache: %w", err)
	}

	e.Preview = json.RawMessage(preview)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)

	// Check expiry
	if time.Now().After(e.ExpiresAt) {
		// Lazy delete
		_, _ = c.db.Exec(`DELETE FROM cache WHERE key = ?`, key)
		return nil, false, nil
	}

	// Increment hit count
	_, _ = c.db.Exec(`UPDATE cache SET hit_count = hit_count + 1 WHERE key = ?`, key)
	e.HitCount++

	return &e, true, nil
}

// Set stores a cache entry with the default TTL.
func (c *Cache) Set(key, capability, argsHash string, preview json.RawMessage, artifactRef, threadID string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	now := time.Now().UTC()
	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO cache
		(key, capability, args_hash, preview, artifact_ref, thread_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, key, capability, argsHash, string(preview), artifactRef, threadID,
		now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339),
	)
	return err
}

// Invalidate removes a cache entry.
func (c *Cache) Invalidate(key string) error {
	_, err := c.db.Exec(`DELETE FROM cache WHERE key = ?`, key)
	return err
}

// Purge removes all expired entries.
func (c *Cache) Purge() (int64, error) {
	res, err := c.db.Exec(`DELETE FROM cache WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Stats returns cache statistics.
func (c *Cache) Stats() (total, expired int64, err error) {
	err = c.db.QueryRow(`SELECT COUNT(*) FROM cache`).Scan(&total)
	if err != nil {
		return
	}
	err = c.db.QueryRow(`SELECT COUNT(*) FROM cache WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339)).Scan(&expired)
	return
}

// normalizeArgs sorts map keys for deterministic hashing.
func normalizeArgs(args any) any {
	if args == nil {
		return nil
	}
	// Marshal and unmarshal to get a map
	data, err := json.Marshal(args)
	if err != nil {
		return args
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return args
	}
	return normalized
}
