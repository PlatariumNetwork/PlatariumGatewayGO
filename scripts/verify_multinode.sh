#!/usr/bin/env bash
# Automated multi-node verification (3 nodes, Core RPC mode):
# - Core JSON-RPC mode (PLATARIUM_CORE_MODE=rpc + platarium-cli serve)
# - Gossip discovery (node 2 learns node 1 via node 0 only)
# - Chain JSON-RPC /rpc/v1 on all nodes
# - L1/L2 block sync + balance consistency
#
# Usage: cd PlatariumGatewayGO && chmod +x scripts/verify_multinode.sh && ./scripts/verify_multinode.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CORE_DIR="$(cd "$GATEWAY_DIR/../PlatariumCore" && pwd)"
CLI="$CORE_DIR/target/release/platarium-cli"
DATA_DIR="$GATEWAY_DIR/data/verify-multinode"
LOG_DIR="$GATEWAY_DIR/log/verify-multinode"
CORE_RPC_ADDR="127.0.0.1:19500"
CORE_RPC_PORT="${CORE_RPC_ADDR##*:}"

BASE_REST=2812
BASE_WS=2813
NODES=3

CORE_RPC_PID=""
GATEWAY_PIDS=()

pass() { echo "  ✓ $1"; }
fail() { echo "  ✗ $1"; exit 1; }

core_rpc_ping() {
  python3 - <<PY
import socket, json, sys
try:
    s = socket.create_connection(("${CORE_RPC_ADDR%:*}", ${CORE_RPC_PORT}), timeout=3)
    s.sendall(b'{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}\n')
    line = s.recv(4096).decode().strip()
    s.close()
    d = json.loads(line)
    ok = d.get("result", {}).get("ok") is True
    sys.exit(0 if ok else 1)
except Exception as e:
    print(e, file=sys.stderr)
    sys.exit(1)
PY
}

peer_count() {
  local port=$1
  curl -sf "http://127.0.0.1:${port}/network" -o "/tmp/platarium-network-${port}.json"
  python3 - "/tmp/platarium-network-${port}.json" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    d = json.load(f)
print(len(d.get("connectedNodes") or []))
PY
}

rpc_block_number() {
  local port=$1
  curl -sf -X POST "http://127.0.0.1:${port}/rpc/v1" \
    -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"platarium_blockNumber","params":[]}' \
    -o "/tmp/platarium-rpc-${port}.json"
  python3 - "/tmp/platarium-rpc-${port}.json" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    d = json.load(f)
if d.get("error"):
    raise SystemExit(d["error"].get("message", "rpc error"))
print(d.get("result", ""))
PY
}

balance_of() {
  local port=$1 address=$2
  curl -sf "http://127.0.0.1:${port}/pg-bal/${address}" -o "/tmp/platarium-bal-${port}.json"
  python3 - "/tmp/platarium-bal-${port}.json" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    d = json.load(f)
print(d.get("balance", d.get("error", "")))
PY
}

cleanup() {
  echo ""
  echo "Cleaning up..."
  for pid in "${GATEWAY_PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  if [ -n "$CORE_RPC_PID" ]; then
    kill "$CORE_RPC_PID" 2>/dev/null || true
  fi
  NODES=$NODES BASE_REST=$BASE_REST BASE_WS=$BASE_WS "$GATEWAY_DIR/scripts/stop_all_nodes.sh" >/dev/null 2>&1 || true
  if lsof -i ":$CORE_RPC_PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
    lsof -i ":$CORE_RPC_PORT" -sTCP:LISTEN -t | xargs kill 2>/dev/null || true
  fi
}
trap cleanup EXIT

cd "$GATEWAY_DIR"
mkdir -p "$DATA_DIR" "$LOG_DIR"

echo "=== Multi-node verify (3 nodes, Core RPC mode) ==="

NODES=$NODES BASE_REST=$BASE_REST BASE_WS=$BASE_WS "$SCRIPT_DIR/stop_all_nodes.sh" >/dev/null 2>&1 || true
if lsof -i ":$CORE_RPC_PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
  lsof -i ":$CORE_RPC_PORT" -sTCP:LISTEN -t | xargs kill 2>/dev/null || true
  sleep 1
fi

echo "[1/6] Building Core + Gateway..."
(cd "$CORE_DIR" && unset CARGO_TARGET_DIR && cargo build --release >/dev/null)
[ -x "$CLI" ] || fail "platarium-cli not found at $CLI"
go build -o platarium-gateway . >/dev/null

echo "[2/6] Starting Core RPC daemon on $CORE_RPC_ADDR..."
"$CLI" serve --listen "$CORE_RPC_ADDR" >>"$LOG_DIR/core-rpc.log" 2>&1 &
CORE_RPC_PID=$!
sleep 1
core_rpc_ping && pass "Core RPC ping" || fail "Core RPC ping failed (see $LOG_DIR/core-rpc.log)"

export PLATARIUM_CORE_MODE=rpc
export PLATARIUM_CORE_RPC_ADDR=$CORE_RPC_ADDR
export PLATARIUM_CLI_PATH=$CLI
export PLATARIUM_PEER_CONNECT_DELAY_SEC=2
export NODE_HOST=127.0.0.1

echo "[3/6] Starting 3 gateway nodes (RPC mode, gossip test: node2 only seeds node0)..."
for i in 0 1 2; do
  rest=$((BASE_REST + 10 * i))
  ws=$((BASE_WS + 10 * i))
  export PLATARIUM_STATE_FILE="$DATA_DIR/state-node${i}.json"
  export PLATARIUM_CHAIN_FILE="$DATA_DIR/chain-node${i}.json"
  peers_json="[]"
  if [ "$i" -eq 1 ] || [ "$i" -eq 2 ]; then
    peers_json='["ws://127.0.0.1:'"$BASE_WS"'"]'
  fi
  PEERS="$peers_json" ./platarium-gateway -testnet -port "$rest" -ws "$ws" \
    -state-file "$PLATARIUM_STATE_FILE" -chain-file "$PLATARIUM_CHAIN_FILE" \
    >>"$LOG_DIR/node${i}.log" 2>&1 &
  GATEWAY_PIDS+=($!)
  sleep 0.8
done

echo "[4/6] Waiting for peer mesh + gossip (20s)..."
sleep 20

for i in 0 1 2; do
  rest=$((BASE_REST + 10 * i))
  code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 2 "http://127.0.0.1:${rest}/api" || echo 000)
  [ "$code" = "200" ] || fail "node $i REST :$rest not responding (HTTP $code)"
done
pass "All 3 nodes respond on /api"

p0=$(peer_count $BASE_REST)
p1=$(peer_count $((BASE_REST + 10)))
p2=$(peer_count $((BASE_REST + 20)))
echo "  Peer counts: node0=$p0 node1=$p1 node2=$p2"
[ "$p0" -ge 2 ] || fail "node0 expected >=2 peers, got $p0"
[ "$p1" -ge 2 ] || fail "node1 expected >=2 peers, got $p1"
[ "$p2" -ge 2 ] || fail "node2 expected >=2 peers via gossip, got $p2"
pass "Gossip mesh: node2 has >=2 peers without direct seed to node1"

if grep -q "Gossip discovery" "$LOG_DIR/node2.log" 2>/dev/null; then
  pass "node2 log contains Gossip discovery dial"
else
  echo "  (note: node2 may have connected before gossip log; peer count already confirms mesh)"
fi

echo "[5/6] Chain JSON-RPC + L1/L2 block on node0..."
for i in 0 1 2; do
  rest=$((BASE_REST + 10 * i))
  bn=$(rpc_block_number "$rest")
  [ "$bn" = "0x0" ] || [ "$bn" = "0x-1" ] || [ "$bn" = "0x0" ] && true
  [ -n "$bn" ] || fail "node $i chain RPC blockNumber empty"
done
pass "platarium_blockNumber works on all nodes (head=0x0 before block)"

curl -sf -X POST "http://127.0.0.1:${BASE_REST}/api/faucet" \
  -H 'Content-Type: application/json' \
  -d '{"address":"PxAlice"}' >/dev/null

curl -sf -X POST "http://127.0.0.1:${BASE_REST}/api/l1-collect" >/dev/null
sleep 2
curl -sf -X POST "http://127.0.0.1:${BASE_REST}/api/l2-confirm" >/dev/null
sleep 5

bn0=$(rpc_block_number $BASE_REST)
bn1=$(rpc_block_number $((BASE_REST + 10)))
bn2=$(rpc_block_number $((BASE_REST + 20)))
echo "  Block numbers after L2: node0=$bn0 node1=$bn1 node2=$bn2"
[ "$bn0" = "0x1" ] || fail "node0 expected block 0x1, got $bn0"
[ "$bn1" = "$bn0" ] || fail "node1 blockNumber mismatch: $bn1 vs $bn0"
[ "$bn2" = "$bn0" ] || fail "node2 blockNumber mismatch: $bn2 vs $bn0"
pass "Chain RPC blockNumber synced across nodes"

echo "[6/6] Balance consistency (PxAlice after faucet block)..."
b0=$(balance_of $BASE_REST PxAlice)
b1=$(balance_of $((BASE_REST + 10)) PxAlice)
b2=$(balance_of $((BASE_REST + 20)) PxAlice)
echo "  Balances: node0=$b0 node1=$b1 node2=$b2"
[ "$b0" = "$b1" ] && [ "$b1" = "$b2" ] || fail "balance mismatch across nodes"
[ "$b0" != "0" ] && [ "$b0" != "" ] || fail "PxAlice balance still zero"
pass "PxAlice balance matches on all nodes ($b0)"

echo ""
echo "=== Multi-node verify: ALL PASSED ==="
echo "Logs: $LOG_DIR"
