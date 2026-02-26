# relay Plugin Guide

Plugins extend relay with new capabilities (tools).

## Capability contract

A capability receives an `InvokeRequest` and must return an `InvokeResult`:

```go
type InvokeRequest struct {
    Capability     string          // e.g. "my.tool"
    ThreadID       string          // calling thread
    Args           json.RawMessage // validated against ArgsSchema
    IdempotencyKey string
    Tenant         string
}

type InvokeResult struct {
    Capability  string
    Preview     json.RawMessage  // small JSON — sent to caller
    ArtifactRef string           // ref to full output — never full content
    CacheHit    bool
    DurationMs  int64
}
```

**Golden rule**: never put large content in `Preview`. Store it as an artifact
and return the `artifact_ref`. This is the entire point of relay.

## Writing a plugin

```go
package myplugin

import (
    "encoding/json"
    "github.com/relaydev/relay/internal/plugins"
)

// 1. Declare the capability
func Cap() *plugins.Capability {
    return &plugins.Capability{
        Name:        "my.summarize",
        Description: "Summarize a stored artifact",
        ArgsSchema:  json.RawMessage(`{
            "type": "object",
            "properties": {
                "artifact_ref": {"type": "string"},
                "max_sentences": {"type": "integer", "default": 3}
            },
            "required": ["artifact_ref"]
        }`),
        Cacheable:   true,
        CacheTTLSec: 600,
    }
}

// 2. Implement the handler
func Handler(storer plugins.ArtifactStorer) plugins.Handler {
    return func(req *plugins.InvokeRequest) (*plugins.InvokeResult, error) {
        var args struct {
            ArtifactRef  string `json:"artifact_ref"`
            MaxSentences int    `json:"max_sentences"`
        }
        if err := json.Unmarshal(req.Args, &args); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }
        if args.MaxSentences <= 0 {
            args.MaxSentences = 3
        }

        // Do the work...
        summary := summarize(args.ArtifactRef, args.MaxSentences)

        // Store full result — never put it inline
        resultRef, _ := storer.StoreText(req.ThreadID, "summary.txt", summary, "my.summarize")

        // Return small preview + ref
        preview, _ := json.Marshal(map[string]any{
            "sentences": args.MaxSentences,
            "length":    len(summary),
        })

        return &plugins.InvokeResult{
            Capability:  req.Capability,
            Preview:     preview,
            ArtifactRef: resultRef,
        }, nil
    }
}
```

## Registering the plugin

In `internal/daemon/server.go`, add to the `RegisterBuiltins` call
(or create a new registration function):

```go
registry.Register(myplugin.Cap(), myplugin.Handler(storer))
```

## Caching guidelines

- Set `Cacheable: true` for deterministic tools (search, fetch with TTL, transform)
- Set `Cacheable: false` for non-deterministic or side-effectful tools
- Choose `CacheTTLSec` based on data freshness:
  - Static content: 86400 (24h)
  - Search indexes: 300 (5min)
  - External APIs: 600 (10min)

## Security guidelines

- Validate all `Args` fields before use
- Sanitize any external content before storing as artifact
- For external HTTP calls, only allow http/https (see `http.fetch`)
- Bound all output sizes before storing
- Never put credentials in cache keys
