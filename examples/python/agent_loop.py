#!/usr/bin/env python3
"""
relay agent loop example: demonstrates the token minimization pattern.

Shows how an agent would use relay to:
1. Load state_header (not full state) for each step
2. Invoke capabilities that return preview + artifact_ref
3. Accumulate results as artifact refs (not pasted content)
4. Track decisions and progress in state (not re-stating in each message)

This is the core pattern that saves tokens:
  NAIVE: agent re-sends all context every hop
  RELAY: agent sends header + refs, daemon manages memory

Run:
    relay up
    python examples/python/agent_loop.py
"""

import sys
import json
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent.parent / "sdk" / "python"))
from relay_client import RelayClient


def simulate_agent_step(client: RelayClient, tid: str, step: int) -> None:
    """
    Each agent step:
    1. Gets state header (small, bounded)
    2. Decides next action based on header
    3. Invokes a capability (gets preview + ref back)
    4. Updates state with results (by ref, not by content)
    """
    # RELAY pattern: only load header, not full state
    header = client.state_header(tid)

    header_size = len(json.dumps(header))
    print(f"  step {step}: header={header_size}b  facts={len(header['top_facts'])}  " +
          f"v{header['version']}")

    # Agent logic: search for information (cap returns preview + ref)
    queries = ["token reduction", "state management", "artifact refs"]
    query = queries[step % len(queries)]

    result = client.cap_invoke(tid, "retrieval.search", {"query": query})
    cache_status = "HIT" if result["cache_hit"] else "miss"
    preview_size = len(json.dumps(result["preview"]))

    print(f"         search={query!r:25} cache={cache_status} preview={preview_size}b " +
          f"ref={result.get('artifact_ref', 'none')[:12] if result.get('artifact_ref') else 'none'}")

    # Update state with result ref (not the content)
    ops = [
        {
            "op": "add",
            "path": "/last_actions/-",
            "value": {
                "at": f"step-{step}",
                "description": f"searched for: {query}",
                "result_ref": result.get("artifact_ref", ""),
            }
        }
    ]
    if result.get("artifact_ref"):
        ops.append({
            "op": "add",
            "path": "/artifacts/-",
            "value": {
                "ref": result["artifact_ref"],
                "type": "tool_output",
                "name": f"search-{query}",
            }
        })

    client.state_patch(tid, ops)


def main():
    client = RelayClient("http://localhost:7474")

    # Check daemon
    try:
        client.health()
    except Exception:
        print("  daemon not running — run 'relay up' first")
        return

    print("  relay agent loop demo")
    print("  demonstrates token minimization via state_ref + artifact_ref")
    print()

    # Create thread
    thread = client.thread_new("agent-loop-demo")
    tid = thread["thread_id"]
    print(f"  thread: {tid}")

    # Seed with some knowledge artifacts
    print("\n  seeding knowledge base...")
    articles = [
        ("doc1.md", "relay reduces token usage by storing artifacts as refs. No re-sending memory."),
        ("doc2.md", "State management in relay uses JSON Patch (RFC 6902) for incremental updates."),
        ("doc3.md", "Artifact refs allow agents to reference large outputs without pasting content."),
        ("doc4.md", "Token reduction is achieved through: (1) bounded headers, (2) artifact refs, (3) caching."),
        ("doc5.md", "Cache keys are computed from tenant + capability + args + scope + version."),
    ]

    for name, content in articles:
        art = client.artifact_put(tid, name, content)
        print(f"    stored {name}: {art['size']}b ref={art['ref'][:12]}")

    # Initialize state
    client.state_patch(tid, [
        {"op": "add", "path": "/facts/-", "value": {"id": "f1", "key": "mode", "value": "research"}},
        {"op": "add", "path": "/facts/-", "value": {"id": "f2", "key": "hop_limit", "value": 20}},
        {"op": "add", "path": "/constraints/-", "value": {
            "id": "c1",
            "description": "Always use artifact_ref instead of pasting content",
            "severity": "hard",
        }},
    ])

    print(f"\n  running agent loop (5 steps)...")
    print(f"  {'STEP':<8}{'HEADER':<12}{'FACTS':<8}{'VERSION':<10}{'ACTION':<60}{'CACHE'}")
    print(f"  {'-'*8}{'-'*12}{'-'*8}{'-'*10}{'-'*60}{'-'*6}")

    for step in range(1, 6):
        simulate_agent_step(client, tid, step)

    # Final report
    print("\n  generating report...")
    report = client.report(tid, "md")
    savings = report["token_savings"]

    print(f"\n  token savings summary:")
    print(f"    naive tokens (if pasted): {savings['naive_tokens']:>8}")
    print(f"    actual tokens (via refs): {savings['actual_tokens']:>8}")
    print(f"    tokens avoided:           {savings['avoided_tokens']:>8}")

    if savings["naive_tokens"] > 0:
        pct = savings["avoided_tokens"] / savings["naive_tokens"] * 100
        print(f"    reduction:                {pct:>7.1f}%")

    print(f"\n  relay show {tid}")
    print(f"  relay open {tid}")


if __name__ == "__main__":
    main()
