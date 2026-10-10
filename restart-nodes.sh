#!/usr/bin/env bash
# Restart both melancholy-testnet nodes from their own folders (node0 / node1).
# Does NOT rebuild — use update-and-restart.sh after git pull.
# cwd must be the node folder before start.sh. A relative data/ from the repo
# root opens a new empty store and the live chain looks wiped.
set -euo pipefail
cd "$(dirname "$0")"

# start_node runs start.sh only after cd into that node folder.
# RESTART_DRY_RUN=1 prints the cwd and does not launch a process.
start_node() {
  local dir="$1"
  if [[ "${RESTART_DRY_RUN:-}" == "1" ]]; then
    ( cd "$dir" && pwd )
    return 0
  fi
  # cwd must be the node folder (node0 or node1), never the repo root.
  ( cd "$dir" && bash ./start.sh )
}

wait_http() {
  local port="$1"
  local name="$2"
  local tries="${3:-60}"
  local i
  for i in $(seq 1 "$tries"); do
    if curl -sf "http://127.0.0.1:${port}/api/stats" >/dev/null 2>&1; then
      echo "${name} ready on :${port} (${i}s)"
      return 0
    fi
    sleep 1
  done
  echo "WARN: ${name} not ready on :${port} after ${tries}s — check node*/log/gateway.log" >&2
  return 1
}

start_node node0
if [[ "${RESTART_DRY_RUN:-}" != "1" ]]; then
  wait_http 1812 node0 90 || true
fi
start_node node1
if [[ "${RESTART_DRY_RUN:-}" == "1" ]]; then
  exit 0
fi
wait_http 1822 node1 90 || true

echo "=== listeners ==="
ss -tlnp 2>/dev/null | grep -E '1812|1813|1822|1823' || true
echo "=== rocks ==="
echo "node0: $PWD/node0/data/rocksdb"
echo "node1: $PWD/node1/data/rocksdb"
echo "=== stats node0 ==="
curl -sS http://127.0.0.1:1812/api/stats | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("lastBlockNumber"),d.get("mempoolCount"),d.get("pendingCount"),d.get("chainTxCount"))' || true
echo "=== stats node1 ==="
curl -sS http://127.0.0.1:1822/api/stats | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("lastBlockNumber"),d.get("mempoolCount"),d.get("pendingCount"),d.get("chainTxCount"))' || true
