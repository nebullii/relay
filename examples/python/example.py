#!/usr/bin/env python3
"""
relay Python example: agent loop showing state_ref + artifact_ref token minimization.

Run:
    relay up                # start daemon first
    python examples/python/example.py
"""

import sys
import json
from pathlib import Path

# Allow running from repo root
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "sdk" / "python"))

from relay_client import RelayClient, RelayAPIError


def main():
    client = RelayClient("http://localhost:7474")

    # Check daemon is running
    try:
        health = client.health()
        print(f"  daemon: {health['status']}")
    except Exception as e:
        print(f"  error: daemon not running — run 'relay up' first")
        print(f"  detail: {e}")
        return

    # ---- Step 1: Create a thread ----
    print("\n1. Creating thread...")
    thread = client.thread_new("python-example")
    tid = thread["thread_id"]
    print(f"   thread_id: {tid}")
    print(f"   state_ref: {thread['state_ref']}")

    # ---- Step 2: Store artifacts (no pasting into prompts) ----
    print("\n2. Storing artifacts...")
    doc1 = client.artifact_put(
        tid,
        "background.md",
        """# Project Background

relay is a developer tool that reduces LLM/agent token exchange.
Instead of re-sending memory, agents reference IDs.

Key principles:
- State stored once, referenced by state_ref
- Tool outputs stored as artifacts, referenced by artifact_ref
- Cache layer prevents duplicate capability invocations
- Bounded state header keeps prompt sizes small
""",
        "markdown",
        "text/markdown",
    )
    print(f"   stored: {doc1['ref']} ({doc1['size']} bytes)")

    doc2 = client.artifact_put(
        tid,
        "requirements.json",
        json.dumps({
            "project": "relay",
            "version": "1.0.0",
            "requirements": [
                "token minimization",
                "artifact storage",
                "state management",
                "caching",
            ],
        }, indent=2),
        "json",
        "application/json",
    )
    print(f"   stored: {doc2['ref']} ({doc2['size']} bytes)")

    # ---- Step 3: Update state with facts (no re-sending content) ----
    print("\n3. Updating state via patch...")
    patch_result = client.state_patch(tid, [
        {
            "op": "add",
            "path": "/facts/-",
            "value": {
                "id": "f1",
                "key": "project_name",
                "value": "relay",
            }
        },
        {
            "op": "add",
            "path": "/facts/-",
            "value": {
                "id": "f2",
                "key": "phase",
                "value": "development",
            }
        },
        {
            "op": "add",
            "path": "/plan/-",
            "value": {
                "id": "p1",
                "step": "Implement core state management",
                "status": "done",
            }
        },
        {
            "op": "add",
            "path": "/plan/-",
            "value": {
                "id": "p2",
                "step": "Add artifact storage",
                "status": "done",
            }
        },
        {
            "op": "add",
            "path": "/plan/-",
            "value": {
                "id": "p3",
                "step": "Ship Python SDK",
                "status": "pending",
            }
        },
        {
            "op": "add",
            "path": "/artifacts/-",
            "value": {"ref": doc1["ref"], "type": "markdown", "name": "background.md"}
        },
        {
            "op": "add",
            "path": "/artifacts/-",
            "value": {"ref": doc2["ref"], "type": "json", "name": "requirements.json"}
        },
    ])
    print(f"   state version: {patch_result['version']}")
    print(f"   state_ref:     {patch_result['state_ref']}")

    # ---- Step 4: Get bounded state header (token-efficient) ----
    print("\n4. Fetching state header (bounded, token-efficient)...")
    header = client.state_header(tid)
    print(f"   version:    {header['version']}")
    print(f"   facts:      {len(header['top_facts'])}")
    print(f"   plan steps: {len(header['next_steps'])}")
    print(f"   artifacts:  {len(header['artifact_refs'])}")
    print()

    # This is what an agent would receive — compact, no full content
    header_json = json.dumps(header, indent=2)
    print(f"   header size: {len(header_json)} chars (vs {doc1['size'] + doc2['size']} chars raw content)")

    # ---- Step 5: Search artifacts ----
    print("\n5. Invoking retrieval.search capability...")
    search_result = client.cap_invoke(tid, "retrieval.search", {
        "query": "token",
        "limit": 5,
    })
    print(f"   cache_hit:    {search_result['cache_hit']}")
    print(f"   artifact_ref: {search_result.get('artifact_ref', 'n/a')}")
    print(f"   duration_ms:  {search_result['duration_ms']}")
    preview = search_result["preview"]
    print(f"   found:        {preview['count']} results")

    # Second call — should be cache hit
    print("\n6. Invoking retrieval.search again (cache hit)...")
    search_result2 = client.cap_invoke(tid, "retrieval.search", {
        "query": "token",
        "limit": 5,
    })
    print(f"   cache_hit:    {search_result2['cache_hit']}")

    # ---- Step 6: Generate report ----
    print("\n7. Generating report...")
    report = client.report(tid, "md")
    print(f"   artifact_ref: {report['artifact_ref']}")
    print(f"   size:         {report['size']} bytes")
    savings = report["token_savings"]
    print(f"   token savings:")
    print(f"     naive:    {savings['naive_tokens']} tokens")
    print(f"     actual:   {savings['actual_tokens']} tokens")
    print(f"     avoided:  {savings['avoided_tokens']} tokens")

    # ---- Step 7: Show events ----
    print("\n8. Events:")
    evs = client.events(tid)
    for ev in evs:
        ts = ev["timestamp"][:19]
        print(f"   {ts}  {ev['type']}")

    print(f"\n  Done. Thread: {tid}")
    print(f"  relay show {tid}")
    print(f"  relay open {tid}")


if __name__ == "__main__":
    main()
