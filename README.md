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
                    │ [3000 tokens]   │                       │ daemon
                    └─────────────────┘              ┌────────▼────────┐
  agent B ◀──────── [re-reads it all] │  agent B ◀── │ preview: {…}    │
                                      │              │ full: abc123    │
                                      │              └─────────────────┘
```

## Why relay exists

Modern agent systems re-send context on every hop. A 10-step agent loop
with 4KB of accumulated context sends ~40KB total — most of it duplicated.
relay cuts this to ~4KB by storing memory once and referencing it by ID.

The savings compound: with caching, a capability invoked 5 times sends
its payload once and serves refs four more times.

---

## 60-second quickstart

```bash
# 1. Build
git clone https://github.com/relaydev/relay
cd relay
make build

# 2. Start
./relay init
./relay up
#  relay daemon started
#  url: http://localhost:7474

# 3. Create a thread
./relay thread new --name "my-first-run"
#  thread_id  550e8400-e29b-41d4-a716-446655440000

# 4. Store an artifact (returns ref, not content)
echo "# My Notes\nHello, relay." > notes.md
./relay artifact put notes.md --thread <thread_id> --type markdown
#  artifact_ref  01914a2b3c4d5e6f7890...

# 5. Search it
./relay cap invoke retrieval.search --thread <thread_id> \
  --json '{"query":"hello"}'
#  artifact_ref  ...
#  preview: {"count":1,"results":[...]}

# 6. Open the UI
./relay open <thread_id>
```

That's it. Your data never left your machine. No signup. No cloud.

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
The ref is sent; the daemon serves content on demand.

### A2A protocol

Internal agent messages use a typed envelope:

```json
{
  "msg_id":          "...",
  "thread_id":       "...",
  "from":            "agent-a",
  "to_capability":   "retrieval.search",
  "type":            "request",
  "schema":          "com.relay.a2a.envelope.v1",
  "payload":         { ... max 16KB ... },
  "idempotency_key": "...",
  "timestamp":       "...",
  "ttl":             300
}
```

Enforced limits: max payload 16KB, notes max 280 chars, big content must be `artifact_ref`.

---

## CLI cheatsheet

```
Setup
  relay init                        Initialize config and storage
  relay up                          Start daemon in background
  relay down                        Stop daemon
  relay status                      Show daemon status
  relay doctor                      Run diagnostics
  relay version                     Print version

Threads
  relay thread new [--name <name>]  Create a thread, print thread_id
  relay runs                        List recent threads
  relay show <thread_id>            Show thread summary
  relay tail <thread_id>            Stream events (like tail -f)
  relay open <thread_id>            Open web UI in browser

State
  relay state header --thread <id>  Get bounded state header (token-efficient)
  relay state patch  --thread <id>  \
    --json '[{"op":"add","path":"/facts/-","value":{"id":"f1",...}}]'

Artifacts
  relay artifact put  <file> --thread <id> [--type <type>]
  relay artifact get  <ref>  --thread <id> [--out <path>]

Capabilities
  relay cap list                    List available capabilities
  relay cap invoke <cap> --thread <id> --json '{"key":"val"}'

Reports & Stats
  relay report <thread_id> [--format md|json]
  relay stats  <thread_id>

Import / Export
  relay export <thread_id> --out bundle.zip
  relay import bundle.zip
```

---

## API

The daemon exposes a REST API at `http://localhost:7474`:

| Method | Path | Description |
|--------|------|-------------|
| POST | `/threads` | Create thread |
| GET  | `/threads` | List threads |
| GET  | `/threads/{id}` | Thread detail |
| GET  | `/threads/{id}/state` | Full state |
| GET  | `/threads/{id}/state/header` | Bounded header |
| POST | `/threads/{id}/state/patch` | Apply JSON Patch |
| POST | `/threads/{id}/artifacts` | Upload artifact |
| GET  | `/threads/{id}/artifacts` | List artifacts |
| GET  | `/threads/{id}/artifacts/{ref}` | Get artifact |
| GET  | `/threads/{id}/artifacts/{ref}?raw=1` | Download content |
| GET  | `/threads/{id}/events` | List events |
| POST | `/cap/invoke` | Invoke capability |
| GET  | `/cap/list` | List capabilities |
| POST | `/reports/{id}` | Generate report |
| GET  | `/health` | Health check |
| GET  | `/version` | Version info |
| GET  | `/ui/` | Web UI |

---

## Python SDK

```python
pip install ./sdk/python

from relay_client import RelayClient

client = RelayClient("http://localhost:7474")

# Create thread
thread = client.thread_new("my-run")
tid = thread["thread_id"]

# Store content as artifact (get ref back, not content)
art = client.artifact_put(tid, "analysis.md", "# Analysis\n...", "markdown")
ref = art["ref"]  # use this, not the content

# Update state by ref
client.state_patch(tid, [
    {"op": "add", "path": "/artifacts/-", {"ref": ref, "type": "markdown"}},
    {"op": "add", "path": "/facts/-",     {"id": "f1", "key": "phase", "value": "done"}},
])

# Get token-efficient header (not full state)
header = client.state_header(tid)

# Invoke capability — returns preview + ref
result = client.cap_invoke(tid, "retrieval.search", {"query": "analysis"})
print(result["preview"])   # small JSON
print(result["artifact_ref"])  # full results, by ref

# Generate report
report = client.report(tid)
print(f"Tokens avoided: {report['token_savings']['avoided_tokens']}")
```

---

## TypeScript SDK

```typescript
import { RelayClient } from "@relay/client";

const client = new RelayClient("http://localhost:7474");

const thread = await client.threadNew("my-run");
const tid = thread.thread_id;

const art = await client.artifactPut(tid, "notes.md", "# Notes\n...", "markdown");
await client.statePatch(tid, [
  { op: "add", path: "/artifacts/-", value: { ref: art.ref, type: "markdown" } },
]);

const header = await client.stateHeader(tid);  // token-efficient
const result = await client.capInvoke(tid, "retrieval.search", { query: "notes" });
console.log(result.preview);

// Stream events
for await (const ev of client.tail(tid)) {
  console.log(ev.type, ev.payload);
}
```

---

## Plugin authoring

Plugins register capabilities with a name, schema, and handler.

```go
// Register a custom capability
cap := &plugins.Capability{
    Name:        "my.tool",
    Description: "Does something useful",
    ArgsSchema:  json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}}}`),
    Cacheable:   true,
    CacheTTLSec: 300,
}

handler := func(req *plugins.InvokeRequest) (*plugins.InvokeResult, error) {
    var args struct{ Input string `json:"input"` }
    json.Unmarshal(req.Args, &args)

    // Do work...
    result := process(args.Input)

    // Store full result as artifact (return preview + ref, not full content)
    ref, _ := storer.StoreText(req.ThreadID, "output.json", result, "my.tool")
    preview, _ := json.Marshal(map[string]any{"summary": result[:100]})

    return &plugins.InvokeResult{
        Capability:  req.Capability,
        Preview:     preview,
        ArtifactRef: ref,
    }, nil
}

registry.Register(cap, handler)
```

Built-in capabilities:
- `retrieval.search` — full-text search over thread artifacts
- `http.fetch` — fetch URL, store body as artifact, return bounded preview

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
- **actual**: header size + preview sizes
- **avoided**: naive − actual

---

## Storage layout

```
~/.relay/
  config.json             Client configuration
  relay.db                SQLite: state, artifacts, events, cache
  daemon.pid              Daemon PID
  daemon.log              Daemon log
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
- Optional API token for local calls (`relay init` generates one)
- Artifact previews are sanitized: prompt injection patterns are stripped
- Hop limits prevent infinite loops (default: 50 hops/thread)
- Payload size limits on A2A messages (default: 16KB)
- v1 is single-tenant ("local"); multi-tenant auth is architected in

---

## Troubleshooting

**Daemon won't start**
```bash
relay doctor       # check ports, permissions, storage
relay status       # check daemon state
cat ~/.relay/daemon.log  # check logs
```

**Port conflict**
relay automatically tries the next available port. Check `relay status` for the actual port.

**Storage issues**
```bash
ls -la ~/.relay/
relay doctor
```

**Permission errors**
```bash
chmod -R 755 ~/.relay/
```

**Reset everything**
```bash
relay down
rm -rf ~/.relay/
relay init
relay up
```

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
