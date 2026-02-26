package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store manages state persistence for threads.
type Store struct {
	db       *sql.DB
	baseDir  string
}

func NewStore(db *sql.DB, baseDir string) *Store {
	return &Store{db: db, baseDir: baseDir}
}

// Init creates the state table if it doesn't exist.
func (s *Store) Init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS states (
			thread_id  TEXT NOT NULL,
			version    INTEGER NOT NULL,
			data       TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (thread_id, version)
		);
		CREATE INDEX IF NOT EXISTS idx_states_thread ON states(thread_id);
	`)
	return err
}

// Get retrieves the latest state for a thread.
func (s *Store) Get(threadID string) (*State, error) {
	var data string
	err := s.db.QueryRow(`
		SELECT data FROM states WHERE thread_id = ? ORDER BY version DESC LIMIT 1
	`, threadID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("state not found for thread %s", threadID)
	}
	if err != nil {
		return nil, fmt.Errorf("query state: %w", err)
	}

	var st State
	if err := json.Unmarshal([]byte(data), &st); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &st, nil
}

// Put stores a new state version.
func (s *Store) Put(st *State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO states (thread_id, version, data, created_at)
		VALUES (?, ?, ?, ?)
	`, st.ThreadID, st.Version, string(data), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert state: %w", err)
	}

	// Also write to filesystem for transparency
	if err := s.writeFile(st, data); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}

	return nil
}

// Patch applies a JSON patch to the current state and stores the result.
func (s *Store) Patch(threadID string, ops []PatchOp) (*State, error) {
	if err := ValidatePatch(ops); err != nil {
		return nil, fmt.Errorf("invalid patch: %w", err)
	}

	current, err := s.Get(threadID)
	if err != nil {
		return nil, err
	}

	next, err := ApplyPatch(current, ops)
	if err != nil {
		return nil, fmt.Errorf("apply patch: %w", err)
	}

	if err := s.Put(next); err != nil {
		return nil, err
	}

	return next, nil
}

// Create initializes a new state for a thread.
func (s *Store) Create(threadID string) (*State, error) {
	st := NewState(threadID)
	if err := s.Put(st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) writeFile(st *State, data []byte) error {
	dir := filepath.Join(s.baseDir, "threads", st.ThreadID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Pretty-print for human readability
	var pretty []byte
	var m any
	if err := json.Unmarshal(data, &m); err == nil {
		pretty, _ = json.MarshalIndent(m, "", "  ")
	} else {
		pretty = data
	}

	path := filepath.Join(dir, "state.json")
	return os.WriteFile(path, pretty, 0644)
}
