#!/usr/bin/env bash
# Ensure nginx (Hestia) then: go build -o platarium-gateway .
set -euo pipefail
cd "$(dirname "$0")/.."

bash scripts/ensure-nginx.sh "$@"
echo "== go build -o platarium-gateway . =="
go build -o platarium-gateway .
echo "✓ built ./platarium-gateway"
