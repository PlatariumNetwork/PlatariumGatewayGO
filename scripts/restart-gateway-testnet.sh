#!/usr/bin/env bash
# Restart both melancholy-testnet gateway nodes.
#
# Canonical chain data lives in PlatariumCore RocksDB (not JSON). Each node opens
# its OWN RocksDB path (single-writer rule) via PLATARIUM_ROCKSDB_PATH.
# JSON chain/state files remain only as legacy/backup + mempool admission input.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
else
  echo "WARN: .env not found in $ROOT — WEBRTC_ICE_SERVERS_JSON may be empty" >&2
fi

# Path to the release platarium-cli that ships RocksDB storage commands.
export PLATARIUM_CLI_PATH="${PLATARIUM_CLI_PATH:-/home/admin/web/rpc-melancholy-testnet.platarium.network/private/PlatariumCore/target/release/platarium-cli}"
export NODE_HOST="${NODE_HOST:-31.172.71.182}"

if [[ ! -x "$PLATARIUM_CLI_PATH" ]]; then
  echo "ERROR: platarium-cli not found or not executable: $PLATARIUM_CLI_PATH" >&2
  echo "Build it: (cd PlatariumCore && cargo build --release)" >&2
  exit 1
fi

go build -o platarium-gateway .
mkdir -p data log

pkill -f './platarium-gateway -testnet' 2>/dev/null || true
sleep 2

# One-time migration helper: if a node's RocksDB is empty but a legacy chain file
# exists, import it once. Safe to re-run (skips when RocksDB already populated).
migrate_if_needed() {
  local db_path="$1" chain_file="$2" state_file="$3"
  if [[ -f "$db_path/CURRENT" ]]; then
    return 0  # RocksDB already initialised
  fi
  if [[ ! -f "$chain_file" ]]; then
    echo "[migrate] no chain file $chain_file; starting with empty RocksDB at $db_path"
    return 0
  fi
  echo "[migrate] importing $chain_file -> $db_path"
  mkdir -p "$db_path"
  "$PLATARIUM_CLI_PATH" migrate-json-to-rocks \
    --db-path "$db_path" \
    --chain-file "$chain_file" \
    ${state_file:+--accounts-file "$state_file"} || {
      echo "[migrate] WARN: migration failed for $db_path (continuing)" >&2
    }
}

# --- node0 (L2 confirmer / single RocksDB writer for its path) ---
export PLATARIUM_STATE_FILE="$ROOT/data/state-node0.json"
export PLATARIUM_CHAIN_FILE="$ROOT/data/chain-node0.json"
export PLATARIUM_ROCKSDB_PATH="$ROOT/data/rocksdb-node0"
migrate_if_needed "$PLATARIUM_ROCKSDB_PATH" "$PLATARIUM_CHAIN_FILE" "$PLATARIUM_STATE_FILE"
nohup ./platarium-gateway -testnet -port 1812 -ws 1813 -state-file "$PLATARIUM_STATE_FILE" >> log/gateway-node0.log 2>&1 &
echo $! > log/gateway-node0.pid
sleep 3

# --- node1 (peer; separate RocksDB path to avoid two writers on one DB) ---
export PLATARIUM_STATE_FILE="$ROOT/data/state-node1.json"
export PLATARIUM_CHAIN_FILE="$ROOT/data/chain-node1.json"
export PLATARIUM_ROCKSDB_PATH="$ROOT/data/rocksdb-node1"
migrate_if_needed "$PLATARIUM_ROCKSDB_PATH" "$PLATARIUM_CHAIN_FILE" "$PLATARIUM_STATE_FILE"
PEERS='["ws://127.0.0.1:1813"]' nohup ./platarium-gateway -testnet -port 1822 -ws 1823 -state-file "$PLATARIUM_STATE_FILE" >> log/gateway-node1.log 2>&1 &
echo $! > log/gateway-node1.pid
sleep 3

echo "=== listeners ==="
ss -tlnp | grep -E '1812|1813|1822|1823' || true
echo "=== rocksdb paths ==="
echo "node0: $ROOT/data/rocksdb-node0"
echo "node1: $ROOT/data/rocksdb-node1"
echo "=== turn-ice node0 ==="
curl -sS "http://127.0.0.1:1812/api/turn-ice" | head -c 400
echo
