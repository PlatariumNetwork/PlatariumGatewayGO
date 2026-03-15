#!/usr/bin/env bash
# Запуск 100 тестових RPC-нод (testnet) однією командою в одному консолі.
# Вікно 1: запустити цей скрипт.
# Вікно 2: запустити scripts/send_tx_load_test.sh для відправки транзакцій та статистики.
#
# Usage:
#   cd PlatariumGatewayGO && chmod +x scripts/run_100nodes.sh && ./scripts/run_100nodes.sh
# Тест лише з 1 нодою:  NODES=1 ./scripts/run_100nodes.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$GATEWAY_DIR/.." && pwd)"
CORE_DIR="$REPO_ROOT/PlatariumCore"
NODES=30
BASE_REST=2812
BASE_WS=2813

# Щоб усі ноди могли відкрити достатньо сокетів (REST + WS + піри)
ulimit -n 2048 2>/dev/null || true

export NODE_HOST="${NODE_HOST:-localhost}"
# Затримка перед підключенням до peers: 2s щоб усі ноди встигли запуститися
export PLATARIUM_PEER_CONNECT_DELAY_SEC="${PLATARIUM_PEER_CONNECT_DELAY_SEC:-2}"

cd "$GATEWAY_DIR"
echo "=== Platarium Testnet: 100 RPC nodes ==="
echo "Gateway: $GATEWAY_DIR"
echo "Nodes:   $NODES (REST ports $BASE_REST..$((BASE_REST+10*(NODES-1))), WS $BASE_WS..$((BASE_WS+10*(NODES-1))))"

# Збірка Core (потрібно для testnet)
if [ -d "$CORE_DIR" ]; then
  echo "[1/3] Building Platarium Core (release)..."
  (cd "$CORE_DIR" && cargo build --release 2>/dev/null) || {
    echo "ERROR: Build PlatariumCore first: cd $CORE_DIR && cargo build --release"
    exit 1
  }
  export PLATARIUM_CLI_PATH="${PLATARIUM_CLI_PATH:-$CORE_DIR/target/release/platarium-cli}"
  [ -x "$PLATARIUM_CLI_PATH" ] || { echo "ERROR: platarium-cli not found at $PLATARIUM_CLI_PATH"; exit 1; }
  echo "       Using CLI: $PLATARIUM_CLI_PATH"
else
  echo "ERROR: PlatariumCore not found at $CORE_DIR"
  exit 1
fi

# Збірка gateway
echo "[2/3] Building gateway..."
go build -o platarium-gateway . || { echo "ERROR: go build failed"; exit 1; }

# Перевірка: усі потрібні порти вільні (REST і WS для кожної ноди)
echo "Checking ports $BASE_REST..$((BASE_REST+10*(NODES-1))) and $BASE_WS..$((BASE_WS+10*(NODES-1)))..."
for (( i=0; i<NODES; i++ )); do
  rest=$((BASE_REST+10*i))
  ws=$((BASE_WS+10*i))
  if (lsof -i ":$rest" -sTCP:LISTEN -t &>/dev/null) || (nc -z localhost "$rest" 2>/dev/null); then
    echo "ERROR: Port $rest (node $i REST) is already in use. Stop existing gateways first:"
    echo "  ./scripts/stop_all_nodes.sh"
    echo "  (or: killall platarium-gateway  /  killall platarium)"
    echo "To see what is using the port: lsof -i :$rest"
    exit 1
  fi
  if (lsof -i ":$ws" -sTCP:LISTEN -t &>/dev/null) || (nc -z localhost "$ws" 2>/dev/null); then
    echo "ERROR: Port $ws (node $i WS) is already in use."
    echo "  ./scripts/stop_all_nodes.sh"
    echo "To see what is using the port: lsof -i :$ws"
    exit 1
  fi
done
echo "  All ports free."

# Повна мережа (full mesh): нода $1 підключається до ВСІХ інших (0..NODES-1 крім себе).
# Це потрібно, щоб L1/L2 голосування доходило до всіх: пропонент розсилає proposal пірам,
# і кожна нода має бути піром пропонента, щоб відправити голос назад.
# Ноди, що ще не запущені, підхоплюються через ConnectToNodeWithRetry (ретраї кожні 5с).
build_peers() {
  local i=$1
  local j list=""
  for (( j=0; j<NODES; j++ )); do
    [ "$j" -eq "$i" ] && continue
    list="${list}\"ws://${NODE_HOST}:$((BASE_WS+10*j))\","
  done
  [ -z "$list" ] && echo "[]" || echo "[${list%,}]"
}

# Щоб було рівно 99 peers (без старих з peers.json) - тимчасово прибираємо peers.json
PEERS_JSON_BACKUP=""
if [ -f "peers.json" ]; then
  mv peers.json peers.json.bak
  PEERS_JSON_BACKUP=1
  echo "  (peers.json тимчасово перейменовано в peers.json.bak - буде відновлено при виході)"
fi

# Логи кожної ноди в окремі файли (щоб бачити чому не стартує, якщо лише 1 працює)
mkdir -p "$GATEWAY_DIR/log"
echo "[3/3] Starting $NODES nodes one by one (logs: log/node_N.log)..."
pids=()
for (( i=0; i<NODES; i++ )); do
  rest=$((BASE_REST+10*i))
  ws=$((BASE_WS+10*i))
  export PEERS
  PEERS="$(build_peers "$i")"
  logfile="$GATEWAY_DIR/log/node_${i}.log"
  ./platarium-gateway -testnet -port "$rest" -ws "$ws" >> "$logfile" 2>&1 &
  pids+=($!)
  # Пауза щоб нода встигла забайндити порт перед стартом наступної (збільшено для стабільності)
  [ "$i" -lt $((NODES-1)) ] && sleep 0.8
done

# Чекаємо поки всі ноди забайндять порти + час на ConnectToPeers (full mesh: ретраї до інших нод кожні 5с)
BIND_WAIT="${BIND_WAIT:-10}"
echo "Waiting for nodes to bind and connect to peers (${PLATARIUM_PEER_CONNECT_DELAY_SEC}s delay + ${BIND_WAIT}s)..."
sleep $((PLATARIUM_PEER_CONNECT_DELAY_SEC + BIND_WAIT))

echo ""
echo "Started ${#pids[@]} processes. Checking how many nodes respond..."
alive=0
for (( i=0; i<NODES; i++ )); do
  port=$((BASE_REST+10*i))
  if curl -s -o /dev/null -w "%{http_code}" --connect-timeout 2 --max-time 3 "http://localhost:$port/api" 2>/dev/null | grep -q 200; then
    (( alive++ )) || true
    echo "  Node $i (port $port): OK"
  else
    echo "  Node $i (port $port): no response (see log/node_${i}.log)"
  fi
done
echo "Nodes responding: $alive/$NODES"
if [ "$alive" -lt "$NODES" ]; then
  echo "  Tip: run 'ulimit -n 2048' in this shell before the script, then run again."
  echo "  Check why a node failed: tail -50 log/node_X.log"
  echo "  Or start fewer nodes: NODES=5 ./scripts/run_100nodes.sh"
fi
echo "  Full mesh: each node connects to all others so L1/L2 votes reach the proposer; if votes still 1, increase BIND_WAIT (e.g. BIND_WAIT=15)."
echo ""
echo "First node: http://localhost:$BASE_REST  ws://localhost:$BASE_WS"
echo "Type 'exit' and Enter to stop all nodes (or Ctrl+C)."
echo ""

cleanup() {
  echo ""
  echo "Stopping all gateway processes..."
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait "${pids[@]}" 2>/dev/null || true
  [ -n "$PEERS_JSON_BACKUP" ] && [ -f "peers.json.bak" ] && mv peers.json.bak peers.json && echo "Restored peers.json"
  echo "Done."
  exit 0
}
trap cleanup SIGINT SIGTERM

while IFS= read -r line; do
  [ "$(echo "$line" | tr -d '[:space:]')" = "exit" ] && break
done
cleanup
