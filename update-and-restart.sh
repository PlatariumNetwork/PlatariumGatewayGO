#!/usr/bin/env bash
# ensure nginx → git pull → build once → copy binary into node0/ and node1/ → restart.
# Runtime data (.env, data/, log/) inside node folders is never overwritten.
set -euo pipefail
cd "$(dirname "$0")"

echo "== nginx (Hestia proxy for RPC domain) =="
bash scripts/ensure-nginx.sh || true

echo "== git pull =="
git fetch origin main
git pull origin main

echo "== build =="
go build -o platarium-gateway .

echo "== stop gateways so the binary can be replaced =="
# SIGTERM often outlives the old 1s pause, and cp then hits ETXTBSY.
pkill -9 -f 'platarium-gateway -testnet' 2>/dev/null || true
pkill -9 -f platarium-cli 2>/dev/null || true
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if ! fuser node0/platarium-gateway node1/platarium-gateway >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

install_gateway_bin() {
  local dest="$1"
  cp -a platarium-gateway "${dest}.new"
  # Unlink the running inode first. Overwriting it in place returns "Text file busy".
  rm -f "$dest"
  mv "${dest}.new" "$dest"
  chmod +x "$dest"
}

echo "== deploy binary to node0 / node1 =="
mkdir -p node0 node1
install_gateway_bin node0/platarium-gateway
install_gateway_bin node1/platarium-gateway

# Optional: share root .env into node folders if they have none yet
if [[ -f .env ]]; then
  [[ -f node0/.env ]] || cp -a .env node0/.env
  [[ -f node1/.env ]] || cp -a .env node1/.env
fi

echo "== restart =="
bash restart-nodes.sh
