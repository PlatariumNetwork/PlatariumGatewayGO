#!/usr/bin/env bash
# git pull → build once → copy binary into node0/ and node1/ → restart.
# Runtime data (.env, data/, log/) inside node folders is never overwritten.
set -euo pipefail
cd "$(dirname "$0")"

echo "== git pull =="
git pull

echo "== build =="
go build -o platarium-gateway .

echo "== deploy binary to node0 / node1 =="
mkdir -p node0 node1
cp -a platarium-gateway node0/platarium-gateway
cp -a platarium-gateway node1/platarium-gateway

# Optional: share root .env into node folders if they have none yet
if [[ -f .env ]]; then
  [[ -f node0/.env ]] || cp -a .env node0/.env
  [[ -f node1/.env ]] || cp -a .env node1/.env
fi

echo "== restart =="
bash restart-nodes.sh
