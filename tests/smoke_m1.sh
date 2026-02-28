#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export ROOT
RELAY_BIN="${ROOT}/relay"
LOG="${ROOT}/tests/smoke_m1_debug.log"
RPB1="${ROOT}/tests/rpb_1.json"
RPB2="${ROOT}/tests/rpb_2.json"
ART="${ROOT}/tests/smoke_1mb.bin"

if [[ -f "${ROOT}/.env" ]]; then
  # shellcheck disable=SC1090
  source "${ROOT}/.env"
fi

if [[ ! -x "$RELAY_BIN" ]]; then
  RELAY_BIN="go run ${ROOT}/cmd/relay"
fi

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  echo "OPENAI_API_KEY is required" >&2
  exit 1
fi

echo "Creating thread..."
THREAD_ID="$($RELAY_BIN thread new --name "smoke-m1" | awk '/thread_id/ {print $2}')"
if [[ -z "$THREAD_ID" ]]; then
  echo "failed to create thread" >&2
  exit 1
fi
echo "thread: $THREAD_ID"

echo "Creating 1MB artifact..."
dd if=/dev/zero of="$ART" bs=1024 count=1024 status=none
$RELAY_BIN artifact put "$ART" --thread "$THREAD_ID" --type binary >/dev/null

MODEL="${RELAY_MODEL:-gpt-5}"
TURNS="${SMOKE_TURNS:-30}"
echo "Running ${TURNS} turns with model: $MODEL"
rm -f "$LOG"
for i in $(seq 1 "$TURNS"); do
  $RELAY_BIN run --thread "$THREAD_ID" --model "$MODEL" --user "turn $i: say ok" --debug 1>/dev/null 2>>"$LOG"
done

echo "Determinism check..."
$RELAY_BIN run --thread "$THREAD_ID" --model "$MODEL" --user "determinism check" --dry-run --dump-rpb "$RPB1"
$RELAY_BIN run --thread "$THREAD_ID" --model "$MODEL" --user "determinism check" --dry-run --dump-rpb "$RPB2"
if ! cmp -s "$RPB1" "$RPB2"; then
  echo "RPB is not deterministic" >&2
  exit 1
fi

if [[ "${SMOKE_REQUIRE_COMPACT:-}" == "1" ]]; then
  echo "Checking compaction..."
  HEADER="$($RELAY_BIN state header --thread "$THREAD_ID")"
  echo "$HEADER" | grep -q "\"session_summary\"" || { echo "compaction did not trigger" >&2; exit 1; }
fi

echo "Validating bounds..."
python3 - <<'PY'
import re, sys, statistics, json, os
root = os.environ.get("ROOT", os.getcwd())
log = os.path.join(root, "tests", "smoke_m1_debug.log")
lines = open(log, "r", encoding="utf-8").read().splitlines()
rows = []
for line in lines:
    m = re.search(r"rpb_bytes=(\d+).*state_header_bytes=(\d+).*preview_bytes=(\d+).*preview_count=(\d+)", line)
    if m:
        rows.append(tuple(map(int, m.groups())))
if not rows:
    print("no debug rows found", file=sys.stderr)
    sys.exit(1)

rpb_bytes = [r[0] for r in rows]
state_header_bytes = [r[1] for r in rows]
preview_bytes = [r[2] for r in rows]
preview_count = [r[3] for r in rows]

if any(x > 2048 for x in state_header_bytes):
    print("state_header exceeds 2048 bytes", file=sys.stderr)
    sys.exit(1)
if any(x > 2048 * 10 for x in preview_bytes):
    print("preview bytes exceed cap", file=sys.stderr)
    sys.exit(1)
if any(x > 10 for x in preview_count):
    print("preview count exceeds 10", file=sys.stderr)
    sys.exit(1)

tail = rpb_bytes[-10:] if len(rpb_bytes) >= 10 else rpb_bytes
if max(tail) - min(tail) > 256:
    print("prompt size did not stabilize (last 10 range > 256 bytes)", file=sys.stderr)
    sys.exit(1)
print("OK")
PY

echo "Smoke test passed."
