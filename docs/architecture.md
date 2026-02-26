# relay Architecture

## Overview

relay is a local-first, embedded-storage developer tool for reducing
LLM/agent token exchange. It is a single Go binary that runs as both
a CLI and a background daemon.

```
┌─────────────────────────────────────────────┐
│                  relay binary                │
│                                              │
│  ┌──────────┐    ┌───────────────────────┐  │
│  │   CLI    │───▶│       Daemon          │  │
│  │ (cobra)  │    │   (HTTP server)       │  │
│  └──────────┘    │                       │  │
│                  │  ┌─────┐  ┌────────┐  │  │
│                  │  │State│  │Artifact│  │  │
│                  │  │Store│  │ Store  │  │  │
│                  │  └──┬──┘  └───┬────┘  │  │
│                  │     └────┬────┘       │  │
│                  │  ┌───────▼───────┐    │  │
│                  │  │    SQLite     │    │  │
│                  │  │  relay.db    │    │  │
│                  │  └───────────────┘    │  │
│                  │                       │  │
│                  │  ┌────────┐           │  │
│                  │  │ Cache  │           │  │
│                  │  └────────┘           │  │
│                  │  ┌────────┐           │  │
│                  │  │Plugins │           │  │
│                  │  │Registry│           │  │
│                  └──────────────────────-┘  │
└─────────────────────────────────────────────┘
                  │
       ┌──────────▼──────────┐
       │  ~/.relay/          │
       │  ├── relay.db       │
       │  ├── config.json    │
       │  └── threads/       │
       │      └── <id>/      │
       │          ├── state.json     │
       │          ├── events.log     │
       │          └── artifacts/     │
       └─────────────────────┘
```

## Component responsibilities

### cmd/relay

Cobra-based CLI. Starts daemon as a subprocess with a PID file.
Communicates with daemon via HTTP. Designed to start, query, and
stop without knowing internals.

### internal/daemon

HTTP server that owns the runtime. Owns all stores.
Routes: threads, state, artifacts, events, cap/invoke, reports, UI.

### internal/state

- `State` struct with schema `com.relay.state.v1`
- `Header()` method returns bounded view (token-efficient)
- `ApplyPatch()` applies RFC 6902-style JSON Patch operations
- SQLite store + filesystem mirror (`state.json`)

### internal/artifacts

- Content-addressed storage: hash = SHA-256 of content
- Preview generation: bounded to `MaxPreviewBytes` (2KB)
- Prompt injection sanitization in previews
- Full-text search over stored artifacts

### internal/events

- Append-only event log (SQLite + `events.log` file)
- Events: thread.created, state.patch.applied, artifact.created,
  capability.invoked, report.generated, checkpoint.created
- `Since(afterID)` for tail/polling

### internal/cache

- Key = SHA-256(tenant + capability + normalized_args + scope + version)
- SQLite-backed with TTL
- Hit/miss tracking
- Lazy expiry on reads

### internal/plugins

- `Registry`: maps capability name → handler
- `Capability`: name, description, args_schema, cacheable, TTL
- Built-ins: `retrieval.search`, `http.fetch`
- Handlers return `InvokeResult`: preview + artifact_ref + cache metadata

### internal/policy

- Default config: max payload 16KB, max notes 280 chars, max hops 50
- `ValidateEnvelope`: validates A2A message structure
- `CheckHopLimit`: prevents infinite loops
- `ACL`: per-capability allow/deny (default: allow all in v1)

## Data flow: capability invocation

```
client POST /cap/invoke
        │
        ▼
[auth check]
        │
        ▼
[hop limit check]   ─── exceeded ──▶ 429 Too Many Requests
        │
        ▼
[cache lookup]      ─── hit ──▶ return cached preview + artifact_ref
        │ miss
        ▼
[handler invocation]
        │
        ▼
[store result as artifact]
        │
        ▼
[cache result]
        │
        ▼
[emit capability.invoked event]
        │
        ▼
return preview + artifact_ref
```

## Token savings accounting

For each thread:
- `naive_tokens`: sum(artifact.size) / 4  (chars-per-token estimate)
- `actual_tokens`: sum(artifact.preview.size) / 4
- `avoided_tokens`: naive - actual

Shown in `relay stats`, `relay report`, and the web UI.

## Extensibility

relay is designed to extend in v2:
- Multi-tenant: add tenant field to all keys and ACLs
- Remote: daemon can run hosted; CLI points to it via config
- Streaming: replace polling `/events?after=` with SSE or WebSocket
- Plugin loading: compile-in for v1; dynamic loading possible via Go plugins
- Replay: checkpoint + bundle export/import enables deterministic re-runs
