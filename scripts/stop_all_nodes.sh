#!/usr/bin/env bash
# Stop all gateway processes listening on Platarium ports (2812, 2813, 2822, ...).
# Use when killall platarium-gateway fails (on macOS the process may be named platarium).
#
# Usage: cd PlatariumGatewayGO && ./scripts/stop_all_nodes.sh
# Custom range: NODES=100 ./scripts/stop_all_nodes.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
NODES="${NODES:-100}"
BASE_REST="${BASE_REST:-2812}"
BASE_WS="${BASE_WS:-2813}"

cd "$GATEWAY_DIR"
echo "Stopping Platarium gateway processes (ports $BASE_REST..$((BASE_REST+10*(NODES-1))), $BASE_WS..$((BASE_WS+10*(NODES-1))))..."

# 1) Try killall by process name (various names)
for name in platarium-gateway platarium; do
  if killall "$name" 2>/dev/null; then
    echo "  killall $name: sent SIGTERM"
  fi
done

# 2) Find and kill all PIDs listening on our ports
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

# Allow processes to shut down gracefully
sleep 1
# If any remain - SIGKILL by port
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
