# relay

**Agents never re-send memory.**

relay is a terminal-first developer tool that drastically reduces LLM/agent token exchange
by replacing agent-to-agent prose with durable state refs, artifact refs, typed schemas,
caching, and trace/replay.

```
                       BEFORE relay                      WITH relay
                    ┌─────────────────┐              ┌─────────────────┐
  agent A ────────▶ │ Here's all the  │  agent A ──▶ │ state_ref: v7   │
                    │ context again:  │              │ artifact_ref:   │
                    │ [1000 tokens]   │              │   abc123        │
                    │ [2000 tokens]   │              └────────┬────────┘
                    │ [3000 tokens]   │                       │ local store
                    └─────────────────┘              ┌────────▼────────┐
  agent B ◀──────── [re-reads it all] │  agent B ◀── │ preview: {…}    │
                                      │              │ full: abc123    │
                                      │              └─────────────────┘
```

## Why relay exists

Modern agent systems re-send context on every hop. A 10-step agent loop
with 4KB of accumulated context sends ~40KB total — most of it duplicated.
relay cuts this to ~4KB by storing memory once and referencing it by ID.

The savings compound as artifacts accumulate and only previews are sent.

---

## 60-second quickstart

```bash
# 1. Build
git clone https://github.com/relaydev/relay
cd relay
go build -o relay ./cmd/relay

# 2. Init local storage
./relay init

# 3. Run a prompt (auto-creates a thread)
./relay "summarize this repo"

# 4. Store an artifact (returns ref, not content)
echo "# My Notes\nHello, relay." > notes.md
./relay thread new --name "my-first-run"
#  thread_id  550e8400-e29b-41d4-a716-446655440000
./relay artifact put notes.md --thread <thread_id> --type markdown
#  artifact_ref  01914a2b3c4d5e6f7890...
```

That's it. Your data never leaves your machine. No signup. No daemon. No cloud.

---

## Core concepts

### Thread

A thread is an execution context with a unique ID. Everything lives inside a thread:
state, artifacts, events. Storage path: `~/.relay/threads/<thread_id>/`.

```
thread_id:   550e8400-e29b-41d4-a716-446655440000
state_ref:   v7
artifacts:   [abc123, def456, ghi789]
events:      [...append-only log...]
```

### state_ref

The state is canonical memory: facts, constraints, open questions, decisions,
plan, and artifact refs. It lives in `~/.relay/threads/<id>/state.json` and in SQLite.

Updates happen **only** via JSON Patch (RFC 6902) — never by overwriting.
The `state_ref` (e.g. `v7`) points to an exact version.

**State header** — the token-efficient view:
```json
{
  "$schema": "com.relay.state.v1",
  "thread_id": "...",
  "version": 7,
  "top_facts":       [...bounded to 10...],
  "top_constraints": [...bounded to 5...],
  "open_questions":  [...open only, bounded to 5...],
  "next_steps":      [...pending only, bounded to 5...],
  "artifact_refs":   [...bounded to 10...],
  "metrics": { "cache_hits": 3, "tokens_avoided": 4200 }
}
```

Agents receive the header, not the full state. This keeps prompts small.

### artifact_ref

An artifact is any stored content: markdown, JSON, HTML, text, binary.
Every artifact has:

```
ref:       01914a2b3c4d5e6f7890abcdef  (opaque ID)
type:      markdown | json | html | text | binary | tool_output | email
preview:   { text: "first 2KB", truncated: true, size: 45000 }
hash:      sha256 hex
provenance: { created_by, created_at, capability }
```

Long content is **never inserted into prompts** by default — only previews.
The ref is sent; content stays on disk.

---

## CLI cheatsheet

```
Setup
  relay init                        Initialize config and storage
  relay version                     Print version

Threads
  relay thread new [--name <name>]  Create a thread, print thread_id
  relay runs                        List recent threads
  relay show <thread_id>            Show thread summary

State
  relay state header --thread <id>  Get bounded state header (token-efficient)
  relay state patch  --thread <id>  \
    --json '[{"op":"add","path":"/facts/-","value":{"id":"f1",...}}]'

Artifacts
  relay artifact put  <file> --thread <id> [--type <type>]
  relay artifact get  <ref>  --thread <id> [--out <path>]

Reports & Stats
  relay report <thread_id> [--format md|json]
  relay stats  <thread_id>
```

---

## Token savings

relay tracks and displays token savings per thread:

```
relay stats <thread_id>

  artifacts                      12
  naive tokens (if pasted)    48320
  actual tokens (via refs)     2400
  tokens avoided              45920
  cache hits                      7
  session cache hits              3
```

The formula:
- **naive**: sum of artifact sizes / 4 (chars-per-token estimate)
- **actual**: preview sizes / 4
- **avoided**: naive − actual

---

## Storage layout

```
~/.relay/
  config.json             Client configuration
  relay.db                SQLite: state, artifacts, events, cache
  threads/
    <thread_id>/
      state.json          Current state (human-readable)
      events.log          Append-only event log (human-readable)
      artifacts/
        <ref>.md          Artifact files
        <ref>.json
        ...
```

Everything is transparent. Open `~/.relay/threads/<id>/state.json` in any editor.

---

## Security model

relay is local-first by design:

- All data stays on your machine in `~/.relay/`
- Artifact previews are sanitized: prompt injection patterns are stripped
- Compaction keeps state bounded over long sessions

---

## Building from source

```bash
git clone https://github.com/relaydev/relay
cd relay

# Requires Go 1.22+, SQLite3
make build          # build binary
make test           # run all tests
make install        # install to $GOPATH/bin
make build-all      # cross-compile for all platforms
```

---

## License

MIT
