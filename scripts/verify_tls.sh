#!/usr/bin/env bash
# TLS multi-node verification (self-signed CA + HTTPS/WSS/P2P wss://).
# Builds on verify_multinode.sh checks with TLS enabled on REST + WebSocket.
#
# Usage: cd PlatariumGatewayGO && chmod +x scripts/verify_tls.sh && ./scripts/verify_tls.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CORE_DIR="$(cd "$GATEWAY_DIR/../PlatariumCore" && pwd)"
CLI="$CORE_DIR/target/release/platarium-cli"
DATA_DIR="$GATEWAY_DIR/data/verify-tls"
LOG_DIR="$GATEWAY_DIR/log/verify-tls"
CERT_DIR="$DATA_DIR/certs"
CORE_RPC_ADDR="127.0.0.1:19500"
CORE_RPC_PORT="${CORE_RPC_ADDR##*}"

BASE_REST=2812
BASE_WS=2813
NODES=3

CORE_RPC_PID=""
GATEWAY_PIDS=()
CURL_TLS=(--cacert "$CERT_DIR/tls-ca.pem" --connect-timeout 3 --max-time 10)

pass() { echo "  ✓ $1"; }
fail() { echo "  ✗ $1"; exit 1; }

generate_certs() {
  mkdir -p "$CERT_DIR"
  local openssl_cfg="$CERT_DIR/openssl.cnf"
  cat >"$openssl_cfg" <<'EOF'
[req]
distinguished_name = req_distinguished_name
x509_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = platarium-test-ca

[v3_req]
subjectAltName = @alt_names
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth

[alt_names]
DNS.1 = localhost
IP.1 = 127.0.0.1
EOF

  # CA
  openssl genrsa -out "$CERT_DIR/tls-ca.key" 2048 2>/dev/null
  openssl req -new -x509 -days 1 -key "$CERT_DIR/tls-ca.key" \
    -out "$CERT_DIR/tls-ca.pem" -subj "/CN=Platarium-Test-CA" 2>/dev/null

  # Server cert signed by CA
  openssl genrsa -out "$CERT_DIR/tls-key.pem" 2048 2>/dev/null
  openssl req -new -key "$CERT_DIR/tls-key.pem" -out "$CERT_DIR/tls.csr" \
    -config "$openssl_cfg" 2>/dev/null
  openssl x509 -req -days 1 -in "$CERT_DIR/tls.csr" \
    -CA "$CERT_DIR/tls-ca.pem" -CAkey "$CERT_DIR/tls-ca.key" -CAcreateserial \
    -out "$CERT_DIR/tls-cert.pem" -extensions v3_req -extfile "$openssl_cfg" 2>/dev/null
}

core_rpc_ping() {
  local host="${CORE_RPC_ADDR%:*}" port="${CORE_RPC_ADDR##*:}" i
  for i in 1 2 3 4 5; do
    if python3 -c "
import socket, json, sys
host, port = sys.argv[1], int(sys.argv[2])
s = socket.create_connection((host, port), timeout=3)
s.sendall(b'{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\",\"params\":{}}\n')
line = s.recv(4096).decode().strip()
s.close()
d = json.loads(line)
sys.exit(0 if d.get('result', {}).get('ok') is True else 1)
" "$host" "$port" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

https_get() {
  local port=$1 path=$2 out=${3:-}
  if [ -n "$out" ]; then
    curl -sf "${CURL_TLS[@]}" "https://127.0.0.1:${port}${path}" -o "$out"
  else
    curl -sf "${CURL_TLS[@]}" "https://127.0.0.1:${port}${path}"
  fi
}

https_post() {
  local port=$1 path=$2 data=$3 out=${4:-}
  if [ -n "$out" ]; then
    curl -sf "${CURL_TLS[@]}" -X POST "https://127.0.0.1:${port}${path}" \
      -H 'Content-Type: application/json' -d "$data" -o "$out"
  else
    curl -sf "${CURL_TLS[@]}" -X POST "https://127.0.0.1:${port}${path}" \
      -H 'Content-Type: application/json' -d "$data"
  fi
}

peer_count() {
  local port=$1
  https_get "$port" "/network" "/tmp/platarium-tls-network-${port}.json"
  python3 - "/tmp/platarium-tls-network-${port}.json" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    d = json.load(f)
print(len(d.get("connectedNodes") or []))
PY
}

rpc_block_number() {
  local port=$1
  https_post "$port" "/rpc/v1" '{"jsonrpc":"2.0","id":1,"method":"platarium_blockNumber","params":[]}' \
    "/tmp/platarium-tls-rpc-${port}.json"
  python3 - "/tmp/platarium-tls-rpc-${port}.json" <<'PY'
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
  https_get "$port" "/pg-bal/${address}" "/tmp/platarium-tls-bal-${port}.json"
  python3 - "/tmp/platarium-tls-bal-${port}.json" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    d = json.load(f)
print(d.get("balance", d.get("error", "")))
PY
}

wss_probe() {
  local port=$1
  python3 - "$port" "$CERT_DIR/tls-ca.pem" <<'PY'
import ssl, socket, sys
port = int(sys.argv[1])
ca = sys.argv[2]
ctx = ssl.create_default_context(cafile=ca)
with ctx.wrap_socket(socket.socket(), server_hostname="127.0.0.1") as s:
    s.settimeout(3)
    s.connect(("127.0.0.1", port))
    s.sendall(b"GET /?peer=1 HTTP/1.1\r\nHost: 127.0.0.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
    resp = s.recv(256).decode(errors="replace")
    if " 101 " in resp or "101 Switching Protocols" in resp:
        sys.exit(0)
    print(resp[:200], file=sys.stderr)
    sys.exit(1)
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
rm -rf "$DATA_DIR" "$LOG_DIR"
mkdir -p "$DATA_DIR" "$LOG_DIR"

echo "=== TLS multi-node verify (HTTPS + WSS + wss peers) ==="

command -v openssl >/dev/null || fail "openssl required"
command -v python3 >/dev/null || fail "python3 required"

NODES=$NODES BASE_REST=$BASE_REST BASE_WS=$BASE_WS "$SCRIPT_DIR/stop_all_nodes.sh" >/dev/null 2>&1 || true
if lsof -i ":$CORE_RPC_PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
  lsof -i ":$CORE_RPC_PORT" -sTCP:LISTEN -t | xargs kill 2>/dev/null || true
  sleep 1
fi

echo "[1/7] Generating self-signed CA + server cert..."
generate_certs
[ -f "$CERT_DIR/tls-cert.pem" ] || fail "cert generation failed"
pass "TLS certs in $CERT_DIR"

echo "[2/7] Building Core + Gateway..."
(cd "$CORE_DIR" && unset CARGO_TARGET_DIR && cargo build --release >/dev/null)
[ -x "$CLI" ] || fail "platarium-cli not found"
go build -o platarium-gateway . >/dev/null

echo "[3/7] Starting Core RPC daemon..."
"$CLI" serve --listen "$CORE_RPC_ADDR" >>"$LOG_DIR/core-rpc.log" 2>&1 &
CORE_RPC_PID=$!
sleep 2
core_rpc_ping && pass "Core RPC ping" || { tail -20 "$LOG_DIR/core-rpc.log" 2>/dev/null; fail "Core RPC ping failed"; }

export PLATARIUM_CORE_MODE=rpc
export PLATARIUM_CORE_RPC_ADDR=$CORE_RPC_ADDR
export PLATARIUM_CLI_PATH=$CLI
export PLATARIUM_PEER_CONNECT_DELAY_SEC=2
export NODE_HOST=127.0.0.1
export PLATARIUM_TLS_CERT="$CERT_DIR/tls-cert.pem"
export PLATARIUM_TLS_KEY="$CERT_DIR/tls-key.pem"
export PLATARIUM_PEER_TLS_CA="$CERT_DIR/tls-ca.pem"

TLS_CERT="$CERT_DIR/tls-cert.pem"
TLS_KEY="$CERT_DIR/tls-key.pem"

echo "[4/7] Starting 3 TLS gateway nodes (wss peers, shared test cert)..."
for i in 0 1 2; do
  rest=$((BASE_REST + 10 * i))
  ws=$((BASE_WS + 10 * i))
  export PLATARIUM_STATE_FILE="$DATA_DIR/state-node${i}.json"
  export PLATARIUM_CHAIN_FILE="$DATA_DIR/chain-node${i}.json"
  peers_json="[]"
  if [ "$i" -eq 1 ] || [ "$i" -eq 2 ]; then
    peers_json='["wss://127.0.0.1:'"$BASE_WS"'"]'
  fi
  PEERS="$peers_json" ./platarium-gateway -testnet -port "$rest" -ws "$ws" \
    -state-file "$PLATARIUM_STATE_FILE" -chain-file "$PLATARIUM_CHAIN_FILE" \
    --tls-cert "$TLS_CERT" --tls-key "$TLS_KEY" \
    >>"$LOG_DIR/node${i}.log" 2>&1 &
  GATEWAY_PIDS+=($!)
  sleep 0.8
done

echo "[5/7] Waiting for TLS peer mesh (25s)..."
sleep 25

echo "[6/7] TLS endpoint checks..."
# Plain HTTP must fail (no TLS listener on REST)
if curl -sf --connect-timeout 2 "http://127.0.0.1:${BASE_REST}/api" >/dev/null 2>&1; then
  fail "plain HTTP should not work when TLS is enabled"
fi
pass "Plain HTTP rejected on REST :$BASE_REST"

code=$(curl -s -o /dev/null -w "%{http_code}" "${CURL_TLS[@]}" "https://127.0.0.1:${BASE_REST}/api" || echo 000)
[ "$code" = "200" ] || fail "HTTPS /api on node0 failed (HTTP $code)"
pass "HTTPS REST /api on node0"

for i in 0 1 2; do
  ws=$((BASE_WS + 10 * i))
  wss_probe "$ws" || fail "WSS upgrade probe failed on port $ws"
done
pass "WSS WebSocket upgrade on all 3 nodes"

p0=$(peer_count $BASE_REST)
p1=$(peer_count $((BASE_REST + 10)))
p2=$(peer_count $((BASE_REST + 20)))
echo "  Peer counts (via HTTPS /network): node0=$p0 node1=$p1 node2=$p2"
[ "$p0" -ge 2 ] && [ "$p1" -ge 2 ] && [ "$p2" -ge 2 ] || fail "TLS peer mesh incomplete"
pass "wss:// peer mesh (gossip) on all nodes"

grep -q "Connected to wss://" "$LOG_DIR/node1.log" 2>/dev/null \
  && pass "node1 log: Connected to wss:// peer" \
  || fail "node1 missing wss peer connection log"

echo "[7/7] Chain RPC over HTTPS + L1/L2 sync..."
bn_before=$(rpc_block_number $BASE_REST)
for i in 0 1 2; do
  rest=$((BASE_REST + 10 * i))
  bn=$(rpc_block_number "$rest")
  [ -n "$bn" ] || fail "chain RPC empty on node $i"
  [ "$bn" = "$bn_before" ] || fail "node $i blockNumber mismatch before L2: $bn vs $bn_before"
done
pass "platarium_blockNumber over HTTPS on all nodes (head=$bn_before before block)"

https_post "$BASE_REST" "/api/faucet" '{"address":"PxAlice"}' >/dev/null
https_post "$BASE_REST" "/api/l1-collect" '{}' >/dev/null
sleep 2
https_post "$BASE_REST" "/api/l2-confirm" '{}' >/dev/null
sleep 8

bn0=$(rpc_block_number $BASE_REST)
bn1=$(rpc_block_number $((BASE_REST + 10)))
bn2=$(rpc_block_number $((BASE_REST + 20)))
echo "  Block numbers after L2: node0=$bn0 node1=$bn1 node2=$bn2"
[ "$bn0" = "$bn1" ] && [ "$bn1" = "$bn2" ] || fail "blockNumber mismatch across nodes"
pass "Chain RPC blockNumber synced across TLS nodes ($bn0)"

block_count=$(https_get $BASE_REST "/api/blocks" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
[ "${block_count:-0}" -ge 1 ] || fail "expected >=1 block in /api/blocks, got $block_count"
pass "HTTPS /api/blocks lists $block_count confirmed block(s)"

b0=$(balance_of $BASE_REST PxAlice)
b1=$(balance_of $((BASE_REST + 10)) PxAlice)
b2=$(balance_of $((BASE_REST + 20)) PxAlice)
echo "  Balances: node0=$b0 node1=$b1 node2=$b2"
[ "$b0" = "$b1" ] && [ "$b1" = "$b2" ] || fail "balance mismatch: $b0 $b1 $b2"
[ "$b0" != "0" ] && [ "$b0" != "" ] || fail "PxAlice balance still zero"
pass "PxAlice balance synced on all TLS nodes ($b0)"

echo ""
echo "=== TLS multi-node verify: ALL PASSED ==="
echo "Certs: $CERT_DIR"
echo "Logs:  $LOG_DIR"
