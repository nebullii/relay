package local

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/relaydev/relay/internal/artifacts"
	"github.com/relaydev/relay/internal/cache"
	"github.com/relaydev/relay/internal/events"
	"github.com/relaydev/relay/internal/lock"
	"github.com/relaydev/relay/internal/state"
	"github.com/relaydev/relay/internal/storage"
)

type Engine struct {
	db      *sql.DB
	baseDir string
	lock    *lock.FileLock

	states *state.Store
	arts   *artifacts.Store
	evs    *events.Store
	cache  *cache.Cache
}

type Thread struct {
	ID        string
	Name      string
	CreatedAt time.Time
	HopCount  int
}

func Open(baseDir string) (*Engine, error) {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".relay")
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	l, err := lock.Acquire(filepath.Join(baseDir, "relay.lock"))
	if err != nil {
		return nil, err
	}
	db, err := storage.OpenDB(baseDir)
	if err != nil {
		_ = l.Release()
		return nil, err
	}

	e := &Engine{
		db:      db,
		baseDir: baseDir,
		lock:    l,
		states:  state.NewStore(db, baseDir),
		arts:    artifacts.NewStore(db, baseDir),
		evs:     events.NewStore(db, baseDir),
		cache:   cache.New(db),
	}

	for _, init := range []func() error{
		e.states.Init,
		e.arts.Init,
		e.evs.Init,
		e.cache.Init,
		e.initThreadsTable,
	} {
		if err := init(); err != nil {
			_ = e.Close()
			return nil, err
		}
	}

	return e, nil
}

func (e *Engine) Close() error {
	if e.db != nil {
		_ = e.db.Close()
	}
	if e.lock != nil {
		return e.lock.Release()
	}
	return nil
}

func (e *Engine) initThreadsTable() error {
	_, err := e.db.Exec(`
		CREATE TABLE IF NOT EXISTS threads (
			id         TEXT PRIMARY KEY,
			name       TEXT,
			created_at TEXT NOT NULL,
			hop_count  INTEGER DEFAULT 0
		);
	`)
	return err
}

func (e *Engine) CreateThread(name string) (*Thread, *state.State, error) {
	threadID := uuid.New().String()
	_, err := e.db.Exec(`
		INSERT INTO threads (id, name, created_at, hop_count)
		VALUES (?, ?, ?, 0)
	`, threadID, name, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, nil, err
	}

	st, err := e.states.Create(threadID)
	if err != nil {
		return nil, nil, err
	}
	_, _ = e.evs.Append(threadID, events.EventThreadCreated, map[string]string{
		"thread_id": threadID,
		"name":      name,
	})

	return &Thread{ID: threadID, Name: name, CreatedAt: time.Now().UTC(), HopCount: 0}, st, nil
}

func (e *Engine) GetThread(threadID string) (*Thread, error) {
	var t Thread
	var created string
	err := e.db.QueryRow(`
		SELECT id, name, created_at, hop_count FROM threads WHERE id = ?
	`, threadID).Scan(&t.ID, &t.Name, &created, &t.HopCount)
	if err != nil {
		return nil, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &t, nil
}

func (e *Engine) ListThreads(limit int) ([]*Thread, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := e.db.Query(`
		SELECT id, name, created_at, hop_count FROM threads ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Thread
	for rows.Next() {
		var t Thread
		var created string
		if err := rows.Scan(&t.ID, &t.Name, &created, &t.HopCount); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, &t)
	}
	return out, nil
}

func (e *Engine) State(threadID string) (*state.State, error) {
	return e.states.Get(threadID)
}

func (e *Engine) PatchState(threadID string, ops []state.PatchOp) (*state.State, error) {
	return e.states.Patch(threadID, ops)
}

func (e *Engine) StateHeader(threadID string) (*state.Header, error) {
	st, err := e.states.Get(threadID)
	if err != nil {
		return nil, err
	}
	h := st.Header()
	return h, nil
}

func (e *Engine) ArtifactPut(threadID, name string, atype artifacts.ArtifactType, mime string, r io.Reader, prov artifacts.Provenance) (*artifacts.Artifact, error) {
	return e.arts.Put(threadID, name, atype, mime, r, prov)
}

func (e *Engine) ArtifactGet(threadID, ref string) (*artifacts.Artifact, error) {
	return e.arts.Get(threadID, ref)
}

func (e *Engine) ArtifactList(threadID string) ([]*artifacts.Artifact, error) {
	return e.arts.List(threadID)
}

func (e *Engine) ArtifactContent(threadID, ref string) ([]byte, error) {
	rc, err := e.arts.Open(threadID, ref)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (e *Engine) Events(threadID string, types []events.EventType, limit int) ([]*events.Event, error) {
	return e.evs.List(threadID, types, limit)
}

func (e *Engine) Cache() *cache.Cache {
	return e.cache
}
