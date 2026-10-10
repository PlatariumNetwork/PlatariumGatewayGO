#!/usr/bin/env bash
# Proves restart-nodes.sh does not invoke start.sh from the repo root.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
out="$(RESTART_DRY_RUN=1 bash "$ROOT/restart-nodes.sh")"
node0="$(printf '%s\n' "$out" | sed -n '1p')"
node1="$(printf '%s\n' "$out" | sed -n '2p')"
case "$node0" in
  "$ROOT") echo "node0 start cwd is the repo root" >&2; exit 1 ;;
  "$ROOT/node0") ;;
  *) echo "unexpected node0 cwd: $node0" >&2; exit 1 ;;
esac
case "$node1" in
  "$ROOT") echo "node1 start cwd is the repo root" >&2; exit 1 ;;
  "$ROOT/node1") ;;
  *) echo "unexpected node1 cwd: $node1" >&2; exit 1 ;;
esac
example="$ROOT/start.sh.example"
grep -q 'cd "$(dirname "$0")"' "$example"
grep -q 'PLATARIUM_STATE_FILE="\$ROOT/data/state.json"' "$example"
grep -q 'PLATARIUM_ROCKSDB_PATH="\$ROOT/data/rocksdb"' "$example"
echo "restart cwd ok"
