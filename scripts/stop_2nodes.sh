#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$GATEWAY_DIR/.env.gateway"
CWD="$GATEWAY_DIR"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
  CWD="${PLATARIUM_GATEWAY_CWD:-$GATEWAY_DIR}"
fi

cd "$CWD"

for idx in 0 1; do
  pid_file="log/gateway-node${idx}.pid"
  if [[ -f "$pid_file" ]]; then
    kill "$(cat "$pid_file")" 2>/dev/null || true
    rm -f "$pid_file"
    echo "Stopped node ${idx}"
  fi
done

pkill -f "$CWD/platarium-gateway -testnet" 2>/dev/null || true
echo "Done."
