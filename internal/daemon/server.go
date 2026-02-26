package daemon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/relaydev/relay/internal/artifacts"
	"github.com/relaydev/relay/internal/cache"
	"github.com/relaydev/relay/internal/events"
	"github.com/relaydev/relay/internal/plugins"
	"github.com/relaydev/relay/internal/policy"
	"github.com/relaydev/relay/internal/state"
)

// Server is the relay daemon HTTP server.
type Server struct {
	db       *sql.DB
	baseDir  string
	apiToken string
	cfg      *policy.Config

	states    *state.Store
	artStore  *artifacts.Store
	evStore   *events.Store
	cacheStore *cache.Cache
	registry  *plugins.Registry

	mux *http.ServeMux
}

// Config for Server construction.
type Config struct {
	BaseDir  string
	APIToken string
	Policy   *policy.Config
}

func New(db *sql.DB, cfg Config) (*Server, error) {
	s := &Server{
		db:       db,
		baseDir:  cfg.BaseDir,
		apiToken: cfg.APIToken,
		cfg:      cfg.Policy,
	}
	if s.cfg == nil {
		s.cfg = policy.DefaultConfig()
	}

	s.states = state.NewStore(db, cfg.BaseDir)
	s.artStore = artifacts.NewStore(db, cfg.BaseDir)
	s.evStore = events.NewStore(db, cfg.BaseDir)
	s.cacheStore = cache.New(db)

	// Initialize all stores
	for _, init := range []func() error{
		s.states.Init,
		s.artStore.Init,
		s.evStore.Init,
		s.cacheStore.Init,
		s.initThreadsTable,
	} {
		if err := init(); err != nil {
			return nil, fmt.Errorf("init store: %w", err)
		}
	}

	// Set up plugin registry with adapters
	s.registry = plugins.NewRegistry()
	searcher := &artifactSearchAdapter{s.artStore}
	storer := &artifactStoreAdapter{s.artStore}
	if err := plugins.RegisterBuiltins(s.registry, searcher, storer); err != nil {
		return nil, fmt.Errorf("register builtins: %w", err)
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) setupRoutes() {
	s.mux = http.NewServeMux()

	// API routes
	s.mux.HandleFunc("/threads", s.authMiddleware(s.handleThreads))
	s.mux.HandleFunc("/threads/", s.authMiddleware(s.handleThread))
	s.mux.HandleFunc("/cap/invoke", s.authMiddleware(s.handleCapInvoke))
	s.mux.HandleFunc("/cap/list", s.authMiddleware(s.handleCapList))
	s.mux.HandleFunc("/reports/", s.authMiddleware(s.handleReport))
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/version", s.handleVersion)

	// UI
	s.mux.HandleFunc("/ui/", s.handleUI)
	s.mux.HandleFunc("/", s.handleRoot)
}

// --- Auth Middleware ---

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiToken != "" {
			token := r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
			if token != s.apiToken {
				writeError(w, http.StatusUnauthorized, "invalid or missing API token")
				return
			}
		}
		next(w, r)
	}
}

// --- Thread Handlers ---

func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createThread(w, r)
	case http.MethodGet:
		s.listThreads(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) {
	// Parse path: /threads/{id}/...
	path := strings.TrimPrefix(r.URL.Path, "/threads/")
	parts := strings.SplitN(path, "/", 3)

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "thread_id required")
		return
	}

	threadID := parts[0]
	sub := ""
	if len(parts) >= 2 {
		sub = parts[1]
	}
	subsub := ""
	if len(parts) >= 3 {
		subsub = parts[2]
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		s.getThread(w, r, threadID)
	case sub == "state" && subsub == "" && r.Method == http.MethodGet:
		s.getState(w, r, threadID)
	case sub == "state" && subsub == "header" && r.Method == http.MethodGet:
		s.getStateHeader(w, r, threadID)
	case sub == "state" && subsub == "patch" && r.Method == http.MethodPost:
		s.patchState(w, r, threadID)
	case sub == "artifacts" && subsub == "" && r.Method == http.MethodPost:
		s.uploadArtifact(w, r, threadID)
	case sub == "artifacts" && subsub == "" && r.Method == http.MethodGet:
		s.listArtifacts(w, r, threadID)
	case sub == "artifacts" && subsub != "":
		s.getArtifact(w, r, threadID, subsub)
	case sub == "events" && r.Method == http.MethodGet:
		s.listEvents(w, r, threadID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	threadID := uuid.New().String()

	_, err := s.db.Exec(`
		INSERT INTO threads (id, name, created_at, hop_count)
		VALUES (?, ?, ?, 0)
	`, threadID, req.Name, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create thread: %v", err))
		return
	}

	st, err := s.states.Create(threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create state: %v", err))
		return
	}

	_, _ = s.evStore.Append(threadID, events.EventThreadCreated, map[string]string{
		"thread_id": threadID,
		"name":      req.Name,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"thread_id":  threadID,
		"name":       req.Name,
		"state_ref":  fmt.Sprintf("v%d", st.Version),
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) listThreads(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, name, created_at, hop_count FROM threads ORDER BY created_at DESC LIMIT 100
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var threads []map[string]any
	for rows.Next() {
		var id, name, createdAt string
		var hopCount int
		if err := rows.Scan(&id, &name, &createdAt, &hopCount); err != nil {
			continue
		}
		threads = append(threads, map[string]any{
			"thread_id":  id,
			"name":       name,
			"created_at": createdAt,
			"hop_count":  hopCount,
		})
	}
	if threads == nil {
		threads = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request, threadID string) {
	var id, name, createdAt string
	var hopCount int
	err := s.db.QueryRow(`SELECT id, name, created_at, hop_count FROM threads WHERE id = ?`, threadID).
		Scan(&id, &name, &createdAt, &hopCount)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	st, _ := s.states.Get(threadID)
	var stateVersion int
	if st != nil {
		stateVersion = st.Version
	}

	arts, _ := s.artStore.List(threadID)
	writeJSON(w, http.StatusOK, map[string]any{
		"thread_id":      id,
		"name":           name,
		"created_at":     createdAt,
		"hop_count":      hopCount,
		"state_version":  stateVersion,
		"artifact_count": len(arts),
	})
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request, threadID string) {
	st, err := s.states.Get(threadID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) getStateHeader(w http.ResponseWriter, r *http.Request, threadID string) {
	st, err := s.states.Get(threadID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st.Header())
}

func (s *Server) patchState(w http.ResponseWriter, r *http.Request, threadID string) {
	var ops []state.PatchOp
	if err := json.NewDecoder(r.Body).Decode(&ops); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid patch JSON: %v", err))
		return
	}

	next, err := s.states.Patch(threadID, ops)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("patch failed: %v", err))
		return
	}

	_, _ = s.evStore.Append(threadID, events.EventStatePatchApplied, map[string]any{
		"ops":     len(ops),
		"version": next.Version,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"version":    next.Version,
		"updated_at": next.UpdatedAt,
		"state_ref":  fmt.Sprintf("v%d", next.Version),
	})
}

func (s *Server) uploadArtifact(w http.ResponseWriter, r *http.Request, threadID string) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		// Try JSON body
		var req struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Mime    string `json:"mime"`
			Content string `json:"content"`
		}
		if err2 := json.NewDecoder(r.Body).Decode(&req); err2 != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		prov := artifacts.Provenance{
			CreatedBy: "api",
			CreatedAt: time.Now().UTC(),
		}
		atype := artifacts.ArtifactType(req.Type)
		if atype == "" {
			atype = artifacts.TypeText
		}
		mime := req.Mime
		if mime == "" {
			mime = "text/plain"
		}
		art, err := s.artStore.Put(threadID, req.Name, atype, mime, strings.NewReader(req.Content), prov)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, _ = s.evStore.Append(threadID, events.EventArtifactCreated, map[string]any{
			"ref":  art.Ref,
			"type": art.Type,
			"size": art.Size,
		})
		writeJSON(w, http.StatusCreated, art)
		return
	}

	// Multipart form
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	atype := artifacts.ArtifactType(r.FormValue("type"))
	if atype == "" {
		atype = artifacts.TypeBinary
	}
	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}

	prov := artifacts.Provenance{
		CreatedBy: "api",
		CreatedAt: time.Now().UTC(),
	}

	art, err := s.artStore.Put(threadID, header.Filename, atype, mime, file, prov)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_, _ = s.evStore.Append(threadID, events.EventArtifactCreated, map[string]any{
		"ref":  art.Ref,
		"type": art.Type,
		"size": art.Size,
	})
	writeJSON(w, http.StatusCreated, art)
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request, threadID string) {
	arts, err := s.artStore.List(threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if arts == nil {
		arts = []*artifacts.Artifact{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": arts})
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request, threadID, ref string) {
	// Check if requesting raw content
	if r.URL.Query().Get("raw") == "1" {
		rc, err := s.artStore.Open(threadID, ref)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		defer rc.Close()
		art, _ := s.artStore.Get(threadID, ref)
		if art != nil {
			w.Header().Set("Content-Type", art.Mime)
		}
		io.Copy(w, rc)
		return
	}

	art, err := s.artStore.Get(threadID, ref)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// Strip internal path before returning
	artCopy := *art
	artCopy.Path = ""
	writeJSON(w, http.StatusOK, artCopy)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request, threadID string) {
	afterID := r.URL.Query().Get("after")
	var limit int = 200
	evs, err := s.evStore.Since(threadID, afterID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if evs == nil {
		evs = []*events.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs})
}

// --- Capability Handlers ---

func (s *Server) handleCapInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req plugins.InvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}

	if req.Capability == "" {
		writeError(w, http.StatusBadRequest, "capability is required")
		return
	}
	if req.ThreadID == "" {
		writeError(w, http.StatusBadRequest, "thread_id is required")
		return
	}
	if req.Tenant == "" {
		req.Tenant = policy.DefaultTenant
	}

	// Check hop limit
	var hopCount int
	s.db.QueryRow(`SELECT hop_count FROM threads WHERE id = ?`, req.ThreadID).Scan(&hopCount)
	if err := policy.CheckHopLimit(hopCount, s.cfg.MaxHops); err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}

	// Check cache
	cap, handler, err := s.registry.Get(req.Capability)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var cacheKey string
	if cap.Cacheable {
		cacheKey, _ = cache.Key(req.Tenant, req.Capability, req.Args, req.ThreadID, "v1")
		if entry, hit, _ := s.cacheStore.Get(cacheKey); hit {
			// Update metrics
			s.db.Exec(`UPDATE threads SET hop_count = hop_count + 1 WHERE id = ?`, req.ThreadID)
			_, _ = s.evStore.Append(req.ThreadID, events.EventCapabilityInvoked, map[string]any{
				"capability": req.Capability,
				"cache_hit":  true,
			})
			writeJSON(w, http.StatusOK, &plugins.InvokeResult{
				Capability:  req.Capability,
				Preview:     entry.Preview,
				ArtifactRef: entry.ArtifactRef,
				CacheHit:    true,
				CacheKey:    cacheKey,
			})
			return
		}
	}

	// Invoke handler
	result, err := handler(&req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("capability error: %v", err))
		return
	}

	// Cache result
	if cap.Cacheable && cacheKey != "" {
		ttl := time.Duration(cap.CacheTTLSec) * time.Second
		_ = s.cacheStore.Set(cacheKey, req.Capability, cacheKey, result.Preview, result.ArtifactRef, req.ThreadID, ttl)
	}
	result.CacheKey = cacheKey

	// Increment hop count
	s.db.Exec(`UPDATE threads SET hop_count = hop_count + 1 WHERE id = ?`, req.ThreadID)

	_, _ = s.evStore.Append(req.ThreadID, events.EventCapabilityInvoked, map[string]any{
		"capability":   req.Capability,
		"cache_hit":    false,
		"artifact_ref": result.ArtifactRef,
		"duration_ms":  result.DurationMs,
	})

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCapList(w http.ResponseWriter, r *http.Request) {
	caps := s.registry.List()
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": caps})
}

// --- Report Handler ---

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	threadID := strings.TrimPrefix(r.URL.Path, "/reports/")
	threadID = strings.TrimSuffix(threadID, "/")
	if threadID == "" {
		writeError(w, http.StatusBadRequest, "thread_id required")
		return
	}

	var req struct {
		Format string `json:"format"` // md | json
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Format == "" {
		req.Format = "md"
	}

	report, err := s.generateReport(threadID, req.Format)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func (s *Server) generateReport(threadID, format string) (map[string]any, error) {
	st, err := s.states.Get(threadID)
	if err != nil {
		return nil, err
	}

	arts, _ := s.artStore.List(threadID)
	evs, _ := s.evStore.List(threadID, nil, 1000)

	// Token savings estimate
	naiveTokens := 0
	for _, a := range arts {
		naiveTokens += int(a.Size) / 4 // ~4 chars per token
	}
	actualTokens := 0
	for _, a := range arts {
		actualTokens += len(a.Preview.Text) / 4
	}
	avoided := naiveTokens - actualTokens

	var content string
	switch format {
	case "json":
		data, _ := json.MarshalIndent(map[string]any{
			"thread_id":      threadID,
			"state":          st,
			"artifact_count": len(arts),
			"event_count":    len(evs),
			"token_savings": map[string]any{
				"naive_tokens":   naiveTokens,
				"actual_tokens":  actualTokens,
				"avoided_tokens": avoided,
			},
		}, "", "  ")
		content = string(data)
	default:
		content = s.buildMarkdownReport(threadID, st, arts, evs, naiveTokens, actualTokens, avoided)
	}

	prov := artifacts.Provenance{
		CreatedBy: "relay",
		CreatedAt: time.Now().UTC(),
	}
	atype := artifacts.TypeMarkdown
	if format == "json" {
		atype = artifacts.TypeJSON
	}
	mime := "text/markdown"
	if format == "json" {
		mime = "application/json"
	}

	art, err := s.artStore.Put(threadID, "report."+format, atype, mime, strings.NewReader(content), prov)
	if err != nil {
		return nil, fmt.Errorf("store report: %w", err)
	}

	_, _ = s.evStore.Append(threadID, events.EventReportGenerated, map[string]any{
		"artifact_ref": art.Ref,
		"format":       format,
	})

	return map[string]any{
		"artifact_ref": art.Ref,
		"format":       format,
		"size":         art.Size,
		"thread_id":    threadID,
		"token_savings": map[string]any{
			"naive_tokens":   naiveTokens,
			"actual_tokens":  actualTokens,
			"avoided_tokens": avoided,
		},
	}, nil
}

func (s *Server) buildMarkdownReport(threadID string, st *state.State, arts []*artifacts.Artifact, evs []*events.Event, naive, actual, avoided int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Relay Report: %s\n\n", threadID))
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	sb.WriteString("## State Summary\n\n")
	sb.WriteString(fmt.Sprintf("- Version: %d\n", st.Version))
	sb.WriteString(fmt.Sprintf("- Facts: %d\n", len(st.Facts)))
	sb.WriteString(fmt.Sprintf("- Constraints: %d\n", len(st.Constraints)))
	sb.WriteString(fmt.Sprintf("- Open Questions: %d\n", len(st.OpenQuestions)))
	sb.WriteString(fmt.Sprintf("- Plan Steps: %d\n", len(st.Plan)))
	sb.WriteString("\n")

	if len(st.Facts) > 0 {
		sb.WriteString("### Key Facts\n\n")
		for _, f := range st.Facts {
			sb.WriteString(fmt.Sprintf("- **%s**: %v\n", f.Key, f.Value))
		}
		sb.WriteString("\n")
	}

	if len(st.Decisions) > 0 {
		sb.WriteString("### Decisions\n\n")
		for _, d := range st.Decisions {
			sb.WriteString(fmt.Sprintf("- %s (confidence: %.2f)\n", d.Description, d.Confidence))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Artifacts\n\n")
	sb.WriteString(fmt.Sprintf("Total artifacts: %d\n\n", len(arts)))
	for _, a := range arts {
		sb.WriteString(fmt.Sprintf("- `%s` — %s (%d bytes)\n", a.Ref, a.Type, a.Size))
	}
	sb.WriteString("\n")

	sb.WriteString("## Token Savings\n\n")
	sb.WriteString(fmt.Sprintf("| Metric | Value |\n|---|---|\n"))
	sb.WriteString(fmt.Sprintf("| Naive tokens (if pasted) | %d |\n", naive))
	sb.WriteString(fmt.Sprintf("| Actual tokens (refs+previews) | %d |\n", actual))
	sb.WriteString(fmt.Sprintf("| Tokens avoided | %d |\n", avoided))
	if naive > 0 {
		pct := float64(avoided) / float64(naive) * 100
		sb.WriteString(fmt.Sprintf("| Reduction %% | %.1f%% |\n", pct))
	}
	sb.WriteString("\n")

	sb.WriteString("## Event Timeline\n\n")
	for _, ev := range evs {
		sb.WriteString(fmt.Sprintf("- `%s` [%s] %s\n",
			ev.Timestamp.Format("15:04:05"), ev.Type, truncate(string(ev.Payload), 80)))
	}
	return sb.String()
}

// --- Health / Version / UI ---

type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Built   string `json:"built"`
}

var BuildVersion = "1.0.0"
var BuildCommit = "dev"
var BuildDate = "unknown"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, VersionInfo{
		Version: BuildVersion,
		Commit:  BuildCommit,
		Built:   BuildDate,
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/ui/", http.StatusFound)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	// Serve embedded UI
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, uiHTML)
}

// --- Helpers ---

func (s *Server) initThreadsTable() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS threads (
			id         TEXT PRIMARY KEY,
			name       TEXT,
			created_at TEXT NOT NULL,
			hop_count  INTEGER DEFAULT 0
		);
	`)
	return err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Adapters to satisfy plugin interfaces

type artifactSearchAdapter struct {
	store *artifacts.Store
}

func (a *artifactSearchAdapter) SearchFull(threadID, query string, limit int) ([]*plugins.SearchResult, error) {
	results, err := a.store.SearchFull(threadID, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*plugins.SearchResult, len(results))
	for i, r := range results {
		out[i] = &plugins.SearchResult{
			Ref:     r.Ref,
			Type:    string(r.Type),
			Name:    r.Name,
			Snippet: r.Snippet,
			Score:   r.Score,
		}
	}
	return out, nil
}

type artifactStoreAdapter struct {
	store *artifacts.Store
}

func (a *artifactStoreAdapter) StoreText(threadID, name, content, capability string) (string, error) {
	prov := artifacts.Provenance{
		CreatedBy:  "relay",
		CreatedAt:  time.Now().UTC(),
		Capability: capability,
	}
	art, err := a.store.Put(threadID, name, artifacts.TypeJSON, "application/json",
		strings.NewReader(content), prov)
	if err != nil {
		return "", err
	}
	return art.Ref, nil
}
