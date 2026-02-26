package events

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const SchemaVersion = "com.relay.event.v1"

// EventType identifies the kind of event.
type EventType string

const (
	EventThreadCreated      EventType = "thread.created"
	EventStateCreated       EventType = "state.created"
	EventStatePatchApplied  EventType = "state.patch.applied"
	EventArtifactCreated    EventType = "artifact.created"
	EventCapabilityInvoked  EventType = "capability.invoked"
	EventMessageReceived    EventType = "message.received"
	EventReportGenerated    EventType = "report.generated"
	EventCheckpointCreated  EventType = "checkpoint.created"
)

// Event is an immutable log entry.
type Event struct {
	ID        string          `json:"id"`
	ThreadID  string          `json:"thread_id"`
	Type      EventType       `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp time.Time       `json:"timestamp"`
}

// Store is an append-only event log.
type Store struct {
	db      *sql.DB
	baseDir string
}

func NewStore(db *sql.DB, baseDir string) *Store {
	return &Store{db: db, baseDir: baseDir}
}

func (s *Store) Init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			id         TEXT PRIMARY KEY,
			thread_id  TEXT NOT NULL,
			type       TEXT NOT NULL,
			payload    TEXT NOT NULL,
			timestamp  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_events_thread ON events(thread_id);
		CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
		CREATE INDEX IF NOT EXISTS idx_events_ts ON events(timestamp);
	`)
	return err
}

// Append adds a new event to the log.
func (s *Store) Append(threadID string, etype EventType, payload any) (*Event, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}

	ev := &Event{
		ID:        newEventID(),
		ThreadID:  threadID,
		Type:      etype,
		Payload:   data,
		Timestamp: time.Now().UTC(),
	}

	_, err = s.db.Exec(`
		INSERT INTO events (id, thread_id, type, payload, timestamp)
		VALUES (?, ?, ?, ?, ?)
	`, ev.ID, ev.ThreadID, ev.Type, string(ev.Payload), ev.Timestamp.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("insert event: %w", err)
	}

	// Also append to human-readable log file
	_ = s.appendFile(threadID, ev)

	return ev, nil
}

// List returns events for a thread, optionally filtered by type.
func (s *Store) List(threadID string, types []EventType, limit int) ([]*Event, error) {
	query := `SELECT id, thread_id, type, payload, timestamp FROM events WHERE thread_id = ?`
	args := []any{threadID}

	if len(types) > 0 {
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		query += " AND type IN (" + strings.Join(placeholders, ",") + ")"
	}

	query += " ORDER BY timestamp ASC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var evs []*Event
	for rows.Next() {
		var ev Event
		var ts string
		var payload string
		if err := rows.Scan(&ev.ID, &ev.ThreadID, &ev.Type, &payload, &ts); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(payload)
		ev.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		evs = append(evs, &ev)
	}
	return evs, nil
}

// Since returns all events after a given event ID.
func (s *Store) Since(threadID, afterID string, limit int) ([]*Event, error) {
	var afterTS string
	if afterID != "" {
		err := s.db.QueryRow(`SELECT timestamp FROM events WHERE id = ?`, afterID).Scan(&afterTS)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}

	query := `SELECT id, thread_id, type, payload, timestamp FROM events WHERE thread_id = ?`
	args := []any{threadID}
	if afterTS != "" {
		query += " AND timestamp > ?"
		args = append(args, afterTS)
	}
	query += " ORDER BY timestamp ASC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var evs []*Event
	for rows.Next() {
		var ev Event
		var ts, payload string
		if err := rows.Scan(&ev.ID, &ev.ThreadID, &ev.Type, &payload, &ts); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(payload)
		ev.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		evs = append(evs, &ev)
	}
	return evs, nil
}

// MarkCheckpoint creates a checkpoint event.
func (s *Store) MarkCheckpoint(threadID, label string) (*Event, error) {
	return s.Append(threadID, EventCheckpointCreated, map[string]string{
		"label": label,
	})
}

func (s *Store) appendFile(threadID string, ev *Event) error {
	dir := filepath.Join(s.baseDir, "threads", threadID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(dir, "events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	line := fmt.Sprintf("[%s] %s %s %s\n",
		ev.Timestamp.Format("2006-01-02T15:04:05.000Z"),
		ev.ID, ev.Type, string(ev.Payload))
	_, err = f.WriteString(line)
	return err
}

var eventCounter int64

func newEventID() string {
	eventCounter++
	return fmt.Sprintf("%013x%06x", time.Now().UnixMilli(), eventCounter&0xFFFFFF)
}
