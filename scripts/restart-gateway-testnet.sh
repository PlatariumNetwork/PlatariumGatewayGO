#!/usr/bin/env bash
# Restart both melancholy-testnet gateway nodes with .env (WEBRTC_ICE_SERVERS_JSON, TURN, etc.)
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

export PLATARIUM_CLI_PATH="${PLATARIUM_CLI_PATH:-/home/admin/web/rpc-melancholy-testnet.platarium.network/private/PlatariumCore/target/release/platarium-cli}"
export NODE_HOST="${NODE_HOST:-31.172.71.182}"

go build -o platarium-gateway .
mkdir -p data log

pkill -f './platarium-gateway -testnet' 2>/dev/null || true
sleep 2

export PLATARIUM_STATE_FILE="$ROOT/data/state-node0.json"
export PLATARIUM_CHAIN_FILE="$ROOT/data/chain-node0.json"
nohup ./platarium-gateway -testnet -port 1812 -ws 1813 -state-file "$PLATARIUM_STATE_FILE" >> log/gateway-node0.log 2>&1 &
echo $! > log/gateway-node0.pid
sleep 3

export PLATARIUM_STATE_FILE="$ROOT/data/state-node1.json"
export PLATARIUM_CHAIN_FILE="$ROOT/data/chain-node1.json"
PEERS='["ws://127.0.0.1:1813"]' nohup ./platarium-gateway -testnet -port 1822 -ws 1823 -state-file "$PLATARIUM_STATE_FILE" >> log/gateway-node1.log 2>&1 &
echo $! > log/gateway-node1.pid
sleep 3

echo "=== listeners ==="
ss -tlnp | grep -E '1812|1813|1822|1823' || true
echo "=== turn-ice node0 ==="
curl -sS "http://127.0.0.1:1812/api/turn-ice" | head -c 400
echo
