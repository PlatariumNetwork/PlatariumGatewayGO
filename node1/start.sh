#!/usr/bin/env bash
# Melancholy testnet — node1 (ports 1822/1823). Peer of node0 WS :1813.
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$PWD"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
elif [[ -f ../.env ]]; then
  set -a
  # shellcheck disable=SC1091
  source ../.env
  set +a
fi

export PLATARIUM_CLI_PATH="${PLATARIUM_CLI_PATH:-/home/admin/web/rpc-melancholy-testnet.platarium.network/private/PlatariumCore/target/release/platarium-cli}"
export NODE_HOST="${NODE_HOST:-31.172.71.182}"
export PLATARIUM_STATE_FILE="$ROOT/data/state.json"
export PLATARIUM_CHAIN_FILE="$ROOT/data/chain.json"
export PLATARIUM_ROCKSDB_PATH="$ROOT/data/rocksdb"
export PEERS="${PEERS:-[\"ws://127.0.0.1:1813\"]}"
# Follower only: node0 is the sole L1/L2 block producer. Dual auto-block races
# drop the same mempool txs on both nodes and makes explorer txs "disappear".
export PLATARIUM_AUTO_BLOCK=0

mkdir -p data/rocksdb log

if [[ ! -x ./platarium-gateway ]]; then
  echo "ERROR: missing ./platarium-gateway — run: bash ../update-and-restart.sh" >&2
  exit 1
fi

pkill -f 'platarium-gateway -testnet -port 1822' 2>/dev/null || true
sleep 1

PEERS="$PEERS" nohup ./platarium-gateway -testnet -port 1822 -ws 1823 -state-file "$PLATARIUM_STATE_FILE" \
  >> log/gateway.log 2>&1 &
echo $! > log/gateway.pid
echo "node1 started pid=$(cat log/gateway.pid) rocks=$PLATARIUM_ROCKSDB_PATH peers=$PEERS"
