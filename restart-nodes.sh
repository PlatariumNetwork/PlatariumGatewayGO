#!/usr/bin/env bash
# Restart both melancholy-testnet nodes from their own folders (node0 / node1).
# Does NOT rebuild — use update-and-restart.sh after git pull.
set -euo pipefail
cd "$(dirname "$0")"

bash node0/start.sh
sleep 3
bash node1/start.sh
sleep 2

echo "=== listeners ==="
ss -tlnp 2>/dev/null | grep -E '1812|1813|1822|1823' || true
echo "=== rocks ==="
echo "node0: $PWD/node0/data/rocksdb"
echo "node1: $PWD/node1/data/rocksdb"
echo "=== stats node0 ==="
curl -sS http://127.0.0.1:1812/api/stats | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("lastBlockNumber"),d.get("mempoolCount"),d.get("pendingCount"))' || true
echo "=== stats node1 ==="
curl -sS http://127.0.0.1:1822/api/stats | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("lastBlockNumber"),d.get("mempoolCount"),d.get("pendingCount"))' || true
