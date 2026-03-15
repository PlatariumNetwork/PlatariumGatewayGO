#!/usr/bin/env bash
# Зупинити всі процеси gateway, що слухають на портах Platarium (2812, 2813, 2822, ...).
# Використовуйте, якщо killall platarium-gateway не спрацьовує (на macOS процес може зватися platarium).
#
# Usage: cd PlatariumGatewayGO && ./scripts/stop_all_nodes.sh
# З іншим діапазоном: NODES=100 ./scripts/stop_all_nodes.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
NODES="${NODES:-100}"
BASE_REST="${BASE_REST:-2812}"
BASE_WS="${BASE_WS:-2813}"

cd "$GATEWAY_DIR"
echo "Stopping Platarium gateway processes (ports $BASE_REST..$((BASE_REST+10*(NODES-1))), $BASE_WS..$((BASE_WS+10*(NODES-1))))..."

# 1) Спробувати killall за іменем (різні варіанти)
for name in platarium-gateway platarium; do
  if killall "$name" 2>/dev/null; then
    echo "  killall $name: sent SIGTERM"
  fi
done

# 2) Знайти та вбити всі PIDs, що слухають на наших портах
killed=0
for (( i=0; i<NODES; i++ )); do
  rest=$((BASE_REST+10*i))
  ws=$((BASE_WS+10*i))
  for port in $rest $ws; do
    pids=$(lsof -i ":$port" -sTCP:LISTEN -t 2>/dev/null)
    for pid in $pids; do
      [ -z "$pid" ] && continue
      if kill "$pid" 2>/dev/null; then
        echo "  killed PID $pid (port $port)"
        (( killed++ )) || true
      fi
    done
  done
done

# Дозволити процесам завершитися
sleep 1
# Якщо ще залишилися - SIGKILL за портами
for (( i=0; i<NODES; i++ )); do
  rest=$((BASE_REST+10*i))
  ws=$((BASE_WS+10*i))
  for port in $rest $ws; do
    pids=$(lsof -i ":$port" -sTCP:LISTEN -t 2>/dev/null)
    for pid in $pids; do
      [ -z "$pid" ] && continue
      kill -9 "$pid" 2>/dev/null && echo "  kill -9 $pid (port $port)"
    done
  done
done

echo "Done. Ports should be free now (check: lsof -i :$BASE_REST)."
