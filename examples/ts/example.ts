/**
 * relay TypeScript example: agent loop with state_ref + artifact_ref token minimization.
 *
 * Run:
 *   relay up
 *   npx ts-node examples/ts/example.ts
 *   # or: deno run --allow-net examples/ts/example.ts
 */

// Using fetch-based client (works in Node 18+, Deno, browser)
// In a real project: import { RelayClient } from "@relay/client"
// Here we inline the client for the example to be self-contained.

const BASE_URL = "http://localhost:7474";

interface Thread {
  thread_id: string;
  state_ref: string;
  name: string;
}

async function apiPost(path: string, body: unknown): Promise<unknown> {
  const resp = await fetch(BASE_URL + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await resp.json();
  if (!resp.ok) throw new Error(`API error ${resp.status}: ${JSON.stringify(data)}`);
  return data;
}

async function apiGet(path: string): Promise<unknown> {
  const resp = await fetch(BASE_URL + path, {
    headers: { "Content-Type": "application/json" },
  });
  const data = await resp.json();
  if (!resp.ok) throw new Error(`API error ${resp.status}: ${JSON.stringify(data)}`);
  return data;
}

async function main() {
  // Check daemon
  try {
    const health = (await apiGet("/health")) as { status: string };
    console.log(`  daemon: ${health.status}`);
  } catch {
    console.error("  error: daemon not running — run 'relay up' first");
    process.exit(1);
  }

  // 1. Create thread
  console.log("\n1. Creating thread...");
  const thread = (await apiPost("/threads", { name: "ts-example" })) as Thread;
  const tid = thread.thread_id;
  console.log(`   thread_id: ${tid}`);
  console.log(`   state_ref: ${thread.state_ref}`);

  // 2. Store artifacts
  console.log("\n2. Storing artifacts...");
  const doc1 = (await apiPost(`/threads/${tid}/artifacts`, {
    name: "spec.md",
    type: "markdown",
    mime: "text/markdown",
    content: `# relay Spec

relay stores agent memory once and references it by ID.
No re-sending. No token waste. Just refs.

## Core Primitives
- **thread**: execution context with unique ID
- **state_ref**: pointer to current memory version
- **artifact_ref**: pointer to stored output
- **capability**: typed tool with preview + ref return
`,
  })) as { ref: string; size: number };
  console.log(`   stored: ${doc1.ref} (${doc1.size} bytes)`);

  // 3. Update state with patch
  console.log("\n3. Patching state...");
  const patchResult = (await apiPost(`/threads/${tid}/state/patch`, [
    { op: "add", path: "/facts/-", value: { id: "f1", key: "language", value: "TypeScript" } },
    { op: "add", path: "/facts/-", value: { id: "f2", key: "sdk_version", value: "1.0.0" } },
    {
      op: "add",
      path: "/decisions/-",
      value: {
        id: "d1",
        description: "Use artifact refs instead of pasting content",
        reason_codes: ["token_reduction", "reusability"],
        confidence: 0.99,
      },
    },
    {
      op: "add",
      path: "/artifacts/-",
      value: { ref: doc1.ref, type: "markdown", name: "spec.md" },
    },
  ])) as { version: number; state_ref: string };
  console.log(`   state version: ${patchResult.version}`);
  console.log(`   state_ref:     ${patchResult.state_ref}`);

  // 4. Get state header (token-efficient view)
  console.log("\n4. Getting state header...");
  const header = (await apiGet(`/threads/${tid}/state/header`)) as {
    version: number;
    top_facts: unknown[];
    next_steps: unknown[];
    artifact_refs: unknown[];
  };
  console.log(`   version:    ${header.version}`);
  console.log(`   facts:      ${header.top_facts.length}`);
  console.log(`   artifacts:  ${header.artifact_refs.length}`);

  // 5. Search capability
  console.log("\n5. Invoking retrieval.search...");
  const search = (await apiPost("/cap/invoke", {
    capability: "retrieval.search",
    thread_id: tid,
    args: { query: "relay token", limit: 5 },
  })) as { cache_hit: boolean; preview: { count: number }; duration_ms: number };
  console.log(`   cache_hit:   ${search.cache_hit}`);
  console.log(`   results:     ${search.preview.count}`);
  console.log(`   duration_ms: ${search.duration_ms}`);

  // Cache hit on second call
  console.log("\n6. Search again (should cache hit)...");
  const search2 = (await apiPost("/cap/invoke", {
    capability: "retrieval.search",
    thread_id: tid,
    args: { query: "relay token", limit: 5 },
  })) as { cache_hit: boolean };
  console.log(`   cache_hit: ${search2.cache_hit}`);

  // 6. Generate report
  console.log("\n7. Generating report...");
  const report = (await apiPost(`/reports/${tid}`, { format: "md" })) as {
    artifact_ref: string;
    size: number;
    token_savings: { naive_tokens: number; actual_tokens: number; avoided_tokens: number };
  };
  console.log(`   artifact_ref: ${report.artifact_ref}`);
  console.log(`   size:         ${report.size} bytes`);
  console.log(`   token savings:`);
  console.log(`     naive:   ${report.token_savings.naive_tokens}`);
  console.log(`     actual:  ${report.token_savings.actual_tokens}`);
  console.log(`     avoided: ${report.token_savings.avoided_tokens}`);

  // 7. Events
  console.log("\n8. Events:");
  const evResult = (await apiGet(`/threads/${tid}/events`)) as {
    events: Array<{ timestamp: string; type: string }>;
  };
  for (const ev of evResult.events) {
    const ts = ev.timestamp.slice(0, 19);
    console.log(`   ${ts}  ${ev.type}`);
  }

  console.log(`\n  Done. Thread: ${tid}`);
  console.log(`  relay show ${tid}`);
  console.log(`  relay open ${tid}`);
}

main().catch((err) => {
  console.error("Error:", err.message);
  process.exit(1);
});
