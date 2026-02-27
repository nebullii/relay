package artifacts

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxPreviewBytes = 2048
	SchemaVersion   = "com.relay.artifact.v1"
)

// Artifact represents a stored artifact.
type Artifact struct {
	Ref        string     `json:"ref"`
	ThreadID   string     `json:"thread_id"`
	Type       ArtifactType `json:"type"`
	Mime       string     `json:"mime"`
	Name       string     `json:"name,omitempty"`
	Size       int64      `json:"size"`
	Hash       string     `json:"hash"` // sha256 hex
	Preview    Preview    `json:"preview"`
	Provenance Provenance `json:"provenance"`
	CreatedAt  time.Time  `json:"created_at"`
	Path       string     `json:"path,omitempty"` // filesystem path (omit from API)
}

type ArtifactType string

const (
	TypeToolOutput ArtifactType = "tool_output"
	TypeEmail      ArtifactType = "email"
	TypeMarkdown   ArtifactType = "markdown"
	TypeJSON       ArtifactType = "json"
	TypeHTML       ArtifactType = "html"
	TypeText       ArtifactType = "text"
	TypeBinary     ArtifactType = "binary"
)

// Preview is a bounded summary of artifact content.
type Preview struct {
	Text      string `json:"text,omitempty"`
	LineCount int    `json:"line_count,omitempty"`
	Truncated bool   `json:"truncated"`
	Size      int64  `json:"size"`
}

// Provenance tracks where an artifact came from.
type Provenance struct {
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	SourceRefs  []string  `json:"source_refs,omitempty"`
	Capability  string    `json:"capability,omitempty"`
}

// Store manages artifact persistence.
type Store struct {
	db      *sql.DB
	baseDir string
}

func NewStore(db *sql.DB, baseDir string) *Store {
	return &Store{db: db, baseDir: baseDir}
}

func (s *Store) Init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS artifacts (
			ref        TEXT PRIMARY KEY,
			thread_id  TEXT NOT NULL,
			type       TEXT NOT NULL,
			mime       TEXT NOT NULL,
			name       TEXT,
			size       INTEGER NOT NULL,
			hash       TEXT NOT NULL,
			preview    TEXT NOT NULL,
			provenance TEXT NOT NULL,
			created_at TEXT NOT NULL,
			path       TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_artifacts_thread ON artifacts(thread_id);
		CREATE INDEX IF NOT EXISTS idx_artifacts_hash ON artifacts(hash);
	`)
	return err
}

// Put stores a new artifact from a reader.
func (s *Store) Put(threadID string, name string, atype ArtifactType, mime string, r io.Reader, prov Provenance) (*Artifact, error) {
	ref := newRef()
	dir := filepath.Join(s.baseDir, "threads", threadID, "artifacts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir artifacts: %w", err)
	}

	ext := extForType(atype, mime)
	path := filepath.Join(dir, ref+ext)

	// Write to temp file first
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("create artifact file: %w", err)
	}

	hasher := sha256.New()
	tee := io.TeeReader(r, hasher)

	// Read all for preview generation too
	content, err := io.ReadAll(tee)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("read artifact content: %w", err)
	}

	if err := os.WriteFile(tmp, content, 0644); err != nil {
		return nil, fmt.Errorf("write artifact: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("rename artifact: %w", err)
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	preview := generatePreview(content, atype)

	art := &Artifact{
		Ref:       ref,
		ThreadID:  threadID,
		Type:      atype,
		Mime:      mime,
		Name:      name,
		Size:      int64(len(content)),
		Hash:      hash,
		Preview:   preview,
		Provenance: prov,
		CreatedAt: time.Now().UTC(),
		Path:      path,
	}

	if err := s.save(art); err != nil {
		os.Remove(path)
		return nil, err
	}

	return art, nil
}

// Get retrieves artifact metadata.
func (s *Store) Get(threadID, ref string) (*Artifact, error) {
	var (
		previewJSON string
		provJSON    string
		createdAt   string
		art         Artifact
	)
	err := s.db.QueryRow(`
		SELECT ref, thread_id, type, mime, name, size, hash, preview, provenance, created_at, path
		FROM artifacts WHERE thread_id = ? AND ref = ?
	`, threadID, ref).Scan(
		&art.Ref, &art.ThreadID, &art.Type, &art.Mime, &art.Name,
		&art.Size, &art.Hash, &previewJSON, &provJSON, &createdAt, &art.Path,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("artifact %s not found", ref)
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact: %w", err)
	}

	if err := json.Unmarshal([]byte(previewJSON), &art.Preview); err != nil {
		return nil, fmt.Errorf("unmarshal preview: %w", err)
	}
	if err := json.Unmarshal([]byte(provJSON), &art.Provenance); err != nil {
		return nil, fmt.Errorf("unmarshal provenance: %w", err)
	}
	art.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &art, nil
}

// List returns all artifacts for a thread, including preview metadata.
func (s *Store) List(threadID string) ([]*Artifact, error) {
	rows, err := s.db.Query(`
		SELECT ref, type, mime, name, size, hash, preview, created_at
		FROM artifacts WHERE thread_id = ? ORDER BY created_at DESC
	`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var arts []*Artifact
	for rows.Next() {
		var art Artifact
		var createdAt, previewJSON string
		if err := rows.Scan(&art.Ref, &art.Type, &art.Mime, &art.Name, &art.Size, &art.Hash, &previewJSON, &createdAt); err != nil {
			return nil, err
		}
		art.ThreadID = threadID
		art.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		_ = json.Unmarshal([]byte(previewJSON), &art.Preview)
		arts = append(arts, &art)
	}
	return arts, nil
}

// Open returns a reader for artifact content.
func (s *Store) Open(threadID, ref string) (io.ReadCloser, error) {
	art, err := s.Get(threadID, ref)
	if err != nil {
		return nil, err
	}
	return os.Open(art.Path)
}

// listWithPaths returns all artifacts including file paths (internal use).
func (s *Store) listWithPaths(threadID string) ([]*Artifact, error) {
	rows, err := s.db.Query(`
		SELECT ref, type, mime, name, size, hash, created_at, path
		FROM artifacts WHERE thread_id = ? ORDER BY created_at DESC
	`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var arts []*Artifact
	for rows.Next() {
		var art Artifact
		var createdAt string
		if err := rows.Scan(&art.Ref, &art.Type, &art.Mime, &art.Name, &art.Size, &art.Hash, &createdAt, &art.Path); err != nil {
			return nil, err
		}
		art.ThreadID = threadID
		art.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		arts = append(arts, &art)
	}
	return arts, nil
}

// SearchFull performs a full-text search across text artifacts for a thread.
func (s *Store) SearchFull(threadID, query string, limit int) ([]*SearchResult, error) {
	arts, err := s.listWithPaths(threadID)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var results []*SearchResult

	for _, art := range arts {
		if art.Type == TypeBinary {
			continue
		}
		if _, err := os.Stat(art.Path); err != nil {
			continue
		}

		content, err := os.ReadFile(art.Path)
		if err != nil {
			continue
		}

		lower := strings.ToLower(string(content))
		if !strings.Contains(lower, query) {
			continue
		}

		// Find first match context
		idx := strings.Index(lower, query)
		start := max(0, idx-100)
		end := min(len(lower), idx+200)
		snippet := string(content)[start:end]

		results = append(results, &SearchResult{
			Ref:     art.Ref,
			Type:    art.Type,
			Name:    art.Name,
			Snippet: strings.TrimSpace(snippet),
			Score:   strings.Count(lower, query),
		})

		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

type SearchResult struct {
	Ref     string       `json:"ref"`
	Type    ArtifactType `json:"type"`
	Name    string       `json:"name"`
	Snippet string       `json:"snippet"`
	Score   int          `json:"score"`
}

func (s *Store) save(art *Artifact) error {
	previewJSON, _ := json.Marshal(art.Preview)
	provJSON, _ := json.Marshal(art.Provenance)

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO artifacts
		(ref, thread_id, type, mime, name, size, hash, preview, provenance, created_at, path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, art.Ref, art.ThreadID, art.Type, art.Mime, art.Name,
		art.Size, art.Hash, string(previewJSON), string(provJSON),
		art.CreatedAt.Format(time.RFC3339), art.Path,
	)
	return err
}

func generatePreview(content []byte, atype ArtifactType) Preview {
	p := Preview{Size: int64(len(content))}

	if atype == TypeBinary {
		p.Text = fmt.Sprintf("[binary, %d bytes]", len(content))
		return p
	}

	if !utf8.Valid(content) {
		p.Text = fmt.Sprintf("[binary data, %d bytes]", len(content))
		p.Truncated = true
		return p
	}

	text := sanitizePreview(string(content))
	lines := strings.Split(text, "\n")
	p.LineCount = len(lines)

	if len(text) > MaxPreviewBytes {
		// Truncate to max bytes (leaving room for "\n...") on a line boundary.
		const ellipsis = "\n..."
		limit := MaxPreviewBytes - len(ellipsis)
		trunc := text[:limit]
		if idx := strings.LastIndex(trunc, "\n"); idx > 0 {
			trunc = trunc[:idx]
		}
		p.Text = trunc + ellipsis
		p.Truncated = true
	} else {
		p.Text = text
	}

	return p
}

// sanitizePreview strips potentially harmful patterns from preview text.
func sanitizePreview(text string) string {
	// Remove common prompt injection patterns
	dangerous := []string{
		"ignore previous instructions",
		"ignore all instructions",
		"<|system|>",
		"<|user|>",
		"<|assistant|>",
		"[INST]",
		"[/INST]",
		"###instruction",
		"###system",
	}

	lower := strings.ToLower(text)
	for _, pattern := range dangerous {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			// Replace with safe marker
			idx := strings.Index(lower, strings.ToLower(pattern))
			text = text[:idx] + "[SANITIZED]" + text[idx+len(pattern):]
			lower = strings.ToLower(text)
		}
	}

	return text
}

func extForType(atype ArtifactType, mime string) string {
	switch atype {
	case TypeMarkdown:
		return ".md"
	case TypeJSON:
		return ".json"
	case TypeHTML:
		return ".html"
	case TypeText, TypeToolOutput:
		return ".txt"
	case TypeEmail:
		return ".eml"
	default:
		if strings.Contains(mime, "pdf") {
			return ".pdf"
		}
		return ".bin"
	}
}

func newRef() string {
	// Simple time-based ULID-style ref
	t := time.Now().UnixMilli()
	r := rand.Uint64() & 0xFFFFFFFFFFFF // 6 bytes random
	return fmt.Sprintf("%013x%012x", t, r)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
