package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/relaydev/relay/internal/daemon"
)

func setupServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	dir, err := os.MkdirTemp("", "relay-integration-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := fmt.Sprintf("%s/relay.db", dir)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv, err := daemon.New(db, daemon.Config{
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return ts, dir
}

func apiRequest(t *testing.T, ts *httptest.Server, method, path string, body any) (map[string]any, int) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, ts.URL+path, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return result, resp.StatusCode
}

func TestIntegration_CreateThread(t *testing.T) {
	ts, _ := setupServer(t)

	result, status := apiRequest(t, ts, "POST", "/threads", map[string]string{"name": "test-thread"})
	if status != 201 {
		t.Fatalf("expected 201, got %d: %v", status, result)
	}

	threadID, ok := result["thread_id"].(string)
	if !ok || threadID == "" {
		t.Fatal("expected thread_id in response")
	}

	stateRef, ok := result["state_ref"].(string)
	if !ok || stateRef == "" {
		t.Fatal("expected state_ref in response")
	}
}

func TestIntegration_ThreadNotFound(t *testing.T) {
	ts, _ := setupServer(t)

	_, status := apiRequest(t, ts, "GET", "/threads/nonexistent", nil)
	if status != 404 {
		t.Errorf("expected 404, got %d", status)
	}
}

func TestIntegration_StateLifecycle(t *testing.T) {
	ts, _ := setupServer(t)

	// Create thread
	result, _ := apiRequest(t, ts, "POST", "/threads", map[string]string{"name": "state-test"})
	threadID := result["thread_id"].(string)

	// Get state header
	header, status := apiRequest(t, ts, "GET", "/threads/"+threadID+"/state/header", nil)
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	if header["thread_id"] != threadID {
		t.Errorf("expected thread_id %s, got %v", threadID, header["thread_id"])
	}

	// Apply patch - add a fact
	patch := []map[string]any{
		{
			"op":   "add",
			"path": "/facts/-",
			"value": map[string]any{
				"id":    "f1",
				"key":   "project",
				"value": "relay",
			},
		},
	}
	patchResult, status := apiRequest(t, ts, "POST", "/threads/"+threadID+"/state/patch", patch)
	if status != 200 {
		t.Fatalf("expected 200, got %d: %v", status, patchResult)
	}

	version, ok := patchResult["version"].(float64)
	if !ok || version < 2 {
		t.Errorf("expected version >= 2, got %v", patchResult["version"])
	}

	// Verify patch was applied
	state, _ := apiRequest(t, ts, "GET", "/threads/"+threadID+"/state", nil)
	facts, ok := state["facts"].([]any)
	if !ok || len(facts) == 0 {
		t.Fatal("expected facts to be populated after patch")
	}
}

func TestIntegration_ArtifactLifecycle(t *testing.T) {
	ts, _ := setupServer(t)

	// Create thread
	result, _ := apiRequest(t, ts, "POST", "/threads", map[string]string{"name": "artifact-test"})
	threadID := result["thread_id"].(string)

	// Upload artifact
	artResult, status := apiRequest(t, ts, "POST", "/threads/"+threadID+"/artifacts", map[string]any{
		"name":    "test-doc.md",
		"type":    "markdown",
		"mime":    "text/markdown",
		"content": "# Test Document\n\nThis is a test artifact for relay.",
	})
	if status != 201 {
		t.Fatalf("expected 201, got %d: %v", status, artResult)
	}

	ref, ok := artResult["ref"].(string)
	if !ok || ref == "" {
		t.Fatal("expected artifact ref")
	}

	// Get artifact metadata
	artMeta, status := apiRequest(t, ts, "GET", "/threads/"+threadID+"/artifacts/"+ref, nil)
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	if artMeta["ref"] != ref {
		t.Errorf("expected ref %s, got %v", ref, artMeta["ref"])
	}

	// List artifacts
	listResult, _ := apiRequest(t, ts, "GET", "/threads/"+threadID+"/artifacts", nil)
	artList, ok := listResult["artifacts"].([]any)
	if !ok || len(artList) == 0 {
		t.Fatal("expected artifacts list")
	}
}

func TestIntegration_CapabilitySearch(t *testing.T) {
	ts, _ := setupServer(t)

	// Create thread and add some content
	result, _ := apiRequest(t, ts, "POST", "/threads", map[string]string{"name": "search-test"})
	threadID := result["thread_id"].(string)

	// Add searchable artifact
	apiRequest(t, ts, "POST", "/threads/"+threadID+"/artifacts", map[string]any{
		"name":    "knowledge.txt",
		"type":    "text",
		"mime":    "text/plain",
		"content": "relay is a tool for reducing LLM token usage via artifact refs and state caching",
	})

	// Invoke search capability
	capResult, status := apiRequest(t, ts, "POST", "/cap/invoke", map[string]any{
		"capability": "retrieval.search",
		"thread_id":  threadID,
		"args":       json.RawMessage(`{"query":"relay token"}`),
	})
	if status != 200 {
		t.Fatalf("expected 200, got %d: %v", status, capResult)
	}

	if capResult["capability"] != "retrieval.search" {
		t.Errorf("unexpected capability: %v", capResult["capability"])
	}
}

func TestIntegration_CapabilityCache(t *testing.T) {
	ts, _ := setupServer(t)

	result, _ := apiRequest(t, ts, "POST", "/threads", map[string]string{"name": "cache-test"})
	threadID := result["thread_id"].(string)

	// Add content
	apiRequest(t, ts, "POST", "/threads/"+threadID+"/artifacts", map[string]any{
		"name": "doc.txt", "type": "text", "mime": "text/plain",
		"content": "token reduction is the goal of relay",
	})

	invokeArgs := map[string]any{
		"capability": "retrieval.search",
		"thread_id":  threadID,
		"args":       json.RawMessage(`{"query":"token"}`),
	}

	// First invocation (miss)
	r1, _ := apiRequest(t, ts, "POST", "/cap/invoke", invokeArgs)
	hit1, _ := r1["cache_hit"].(bool)

	// Second invocation (should be cache hit)
	r2, _ := apiRequest(t, ts, "POST", "/cap/invoke", invokeArgs)
	hit2, _ := r2["cache_hit"].(bool)

	if hit1 {
		t.Error("first invocation should be cache miss")
	}
	if !hit2 {
		t.Error("second invocation should be cache hit")
	}
}

func TestIntegration_EventLog(t *testing.T) {
	ts, _ := setupServer(t)

	result, _ := apiRequest(t, ts, "POST", "/threads", map[string]string{"name": "events-test"})
	threadID := result["thread_id"].(string)

	// Perform some actions
	apiRequest(t, ts, "POST", "/threads/"+threadID+"/artifacts", map[string]any{
		"name": "file.txt", "type": "text", "mime": "text/plain", "content": "hello",
	})
	apiRequest(t, ts, "POST", "/threads/"+threadID+"/state/patch", []map[string]any{
		{"op": "add", "path": "/facts/-", "value": map[string]any{"id": "f1", "key": "k", "value": "v"}},
	})

	evResult, status := apiRequest(t, ts, "GET", "/threads/"+threadID+"/events", nil)
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}

	evs, ok := evResult["events"].([]any)
	if !ok || len(evs) < 2 {
		t.Errorf("expected at least 2 events, got %v", evResult["events"])
	}
}

func TestIntegration_ReportGeneration(t *testing.T) {
	ts, _ := setupServer(t)

	result, _ := apiRequest(t, ts, "POST", "/threads", map[string]string{"name": "report-test"})
	threadID := result["thread_id"].(string)

	// Content must exceed MaxPreviewBytes (2048) so the preview is truncated
	// and avoided_tokens > 0. Use ~4KB to ensure a meaningful reduction.
	largeContent := strings.Repeat("# Analysis\n\nThis is analysis content that exceeds the preview cap.\n\n", 60)

	apiRequest(t, ts, "POST", "/threads/"+threadID+"/artifacts", map[string]any{
		"name": "analysis.md", "type": "markdown", "mime": "text/markdown",
		"content": largeContent,
	})

	reportResult, status := apiRequest(t, ts, "POST", "/reports/"+threadID, map[string]string{"format": "md"})
	if status != 200 {
		t.Fatalf("expected 200, got %d: %v", status, reportResult)
	}

	if reportResult["artifact_ref"] == nil {
		t.Error("expected artifact_ref in report result")
	}
	if reportResult["format"] != "md" {
		t.Errorf("expected format md, got %v", reportResult["format"])
	}

	// Verify token savings are computed and that truncation produced real savings.
	// naive_tokens counts the full artifact size; actual_tokens counts the preview
	// bytes actually sent. For content > MaxPreviewBytes, avoided_tokens must be > 0.
	savings, ok := reportResult["token_savings"].(map[string]any)
	if !ok {
		t.Fatal("expected token_savings in report result")
	}
	naiveTokens, _ := savings["naive_tokens"].(float64)
	actualTokens, _ := savings["actual_tokens"].(float64)
	avoidedTokens, _ := savings["avoided_tokens"].(float64)

	if naiveTokens <= 0 {
		t.Errorf("expected naive_tokens > 0, got %v", naiveTokens)
	}
	if actualTokens <= 0 {
		t.Errorf("expected actual_tokens > 0, got %v", actualTokens)
	}
	if avoidedTokens <= 0 {
		t.Errorf("expected avoided_tokens > 0 for artifact exceeding MaxPreviewBytes, got %v", avoidedTokens)
	}
	if avoidedTokens >= naiveTokens {
		t.Errorf("avoided_tokens (%v) should be less than naive_tokens (%v)", avoidedTokens, naiveTokens)
	}
}

func TestIntegration_Health(t *testing.T) {
	ts, _ := setupServer(t)

	result, status := apiRequest(t, ts, "GET", "/health", nil)
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
}
