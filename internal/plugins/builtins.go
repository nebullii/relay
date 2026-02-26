package plugins

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RegisterBuiltins registers all built-in capabilities.
// The retrieval.search handler needs an artifact searcher; http.fetch is self-contained.
func RegisterBuiltins(r *Registry, searcher ArtifactSearcher, artifactStorer ArtifactStorer) error {
	if err := r.Register(retrivalSearchCap(), retrievalSearchHandler(searcher, artifactStorer)); err != nil {
		return fmt.Errorf("register retrieval.search: %w", err)
	}
	if err := r.Register(httpFetchCap(), httpFetchHandler(artifactStorer)); err != nil {
		return fmt.Errorf("register http.fetch: %w", err)
	}
	return nil
}

// ArtifactSearcher searches artifacts.
type ArtifactSearcher interface {
	SearchFull(threadID, query string, limit int) ([]*SearchResult, error)
}

// SearchResult is used internally.
type SearchResult struct {
	Ref     string `json:"ref"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Snippet string `json:"snippet"`
	Score   int    `json:"score"`
}

// ArtifactStorer stores new artifacts.
type ArtifactStorer interface {
	StoreText(threadID, name, content, capability string) (string, error)
}

// ---- retrieval.search ----

func retrivalSearchCap() *Capability {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query"},
			"limit": {"type": "integer", "default": 10}
		},
		"required": ["query"]
	}`)
	return &Capability{
		Name:        "retrieval.search",
		Description: "Full-text search over stored artifacts in a thread",
		ArgsSchema:  schema,
		Cacheable:   true,
		CacheTTLSec: 300,
	}
}

type searchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func retrievalSearchHandler(searcher ArtifactSearcher, storer ArtifactStorer) Handler {
	return func(req *InvokeRequest) (*InvokeResult, error) {
		var args searchArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if args.Query == "" {
			return nil, fmt.Errorf("query is required")
		}
		if args.Limit <= 0 {
			args.Limit = 10
		}
		if args.Limit > 50 {
			args.Limit = 50
		}

		start := time.Now()
		results, err := searcher.SearchFull(req.ThreadID, args.Query, args.Limit)
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}

		preview := map[string]any{
			"query":   args.Query,
			"count":   len(results),
			"results": results,
		}

		previewJSON, _ := json.Marshal(preview)

		// Store full results as artifact
		fullJSON, _ := json.MarshalIndent(map[string]any{
			"query":   args.Query,
			"results": results,
		}, "", "  ")

		artifactRef, err := storer.StoreText(req.ThreadID, "search-"+args.Query, string(fullJSON), "retrieval.search")
		if err != nil {
			artifactRef = ""
		}

		return &InvokeResult{
			Capability:  req.Capability,
			Preview:     previewJSON,
			ArtifactRef: artifactRef,
			DurationMs:  time.Since(start).Milliseconds(),
		}, nil
	}
}

// ---- http.fetch ----

func httpFetchCap() *Capability {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"url":          {"type": "string", "description": "URL to fetch"},
			"method":       {"type": "string", "default": "GET"},
			"preview_size": {"type": "integer", "default": 512, "description": "Max preview chars"}
		},
		"required": ["url"]
	}`)
	return &Capability{
		Name:        "http.fetch",
		Description: "Fetch a URL and store the response body as an artifact",
		ArgsSchema:  schema,
		Cacheable:   true,
		CacheTTLSec: 600,
	}
}

type fetchArgs struct {
	URL         string `json:"url"`
	Method      string `json:"method"`
	PreviewSize int    `json:"preview_size"`
}

func httpFetchHandler(storer ArtifactStorer) Handler {
	return func(req *InvokeRequest) (*InvokeResult, error) {
		var args fetchArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if args.URL == "" {
			return nil, fmt.Errorf("url is required")
		}
		if args.Method == "" {
			args.Method = "GET"
		}
		if args.PreviewSize <= 0 {
			args.PreviewSize = 512
		}
		if args.PreviewSize > 4096 {
			args.PreviewSize = 4096
		}

		// Security: only allow http/https
		if !strings.HasPrefix(args.URL, "http://") && !strings.HasPrefix(args.URL, "https://") {
			return nil, fmt.Errorf("only http/https URLs are supported")
		}

		start := time.Now()

		client := &http.Client{Timeout: 30 * time.Second}
		httpReq, err := http.NewRequest(args.Method, args.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("User-Agent", "relay/1.0")

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("http request: %w", err)
		}
		defer resp.Body.Close()

		// Read up to 10MB
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		contentType := resp.Header.Get("Content-Type")
		previewText := string(body)
		if len(previewText) > args.PreviewSize {
			previewText = previewText[:args.PreviewSize]
		}

		preview := map[string]any{
			"url":          args.URL,
			"status":       resp.StatusCode,
			"content_type": contentType,
			"size":         len(body),
			"preview":      previewText,
			"truncated":    len(body) > args.PreviewSize,
		}
		previewJSON, _ := json.Marshal(preview)

		// Store full response as artifact
		artifactRef, err := storer.StoreText(req.ThreadID, "fetch-"+safeFilename(args.URL), string(body), "http.fetch")
		if err != nil {
			artifactRef = ""
		}

		return &InvokeResult{
			Capability:  req.Capability,
			Preview:     previewJSON,
			ArtifactRef: artifactRef,
			DurationMs:  time.Since(start).Milliseconds(),
		}, nil
	}
}

func safeFilename(url string) string {
	// Replace unsafe chars
	r := strings.NewReplacer(
		"https://", "", "http://", "",
		"/", "-", "?", "-", "&", "-", "=", "-",
		".", "-", ":", "-",
	)
	name := r.Replace(url)
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}
