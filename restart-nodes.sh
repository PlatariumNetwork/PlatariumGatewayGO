#!/usr/bin/env bash
# Restart both melancholy-testnet nodes from their own folders (node0 / node1).
# Does NOT rebuild — use update-and-restart.sh after git pull.
set -euo pipefail
cd "$(dirname "$0")"

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

bash node0/start.sh
wait_http 1812 node0 90 || true
bash node1/start.sh
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
