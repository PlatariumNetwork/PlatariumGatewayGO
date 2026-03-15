#!/usr/bin/env bash
# Run test network: build Platarium Core, start gateway in testnet mode, run Core validation tests.
# Usage: from PlatariumNetwork repo root or from PlatariumGatewayGO:
#   ./PlatariumGatewayGO/scripts/run_testnet.sh
#   cd PlatariumGatewayGO && ./scripts/run_testnet.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$GATEWAY_DIR/.." && pwd)"
CORE_DIR="$REPO_ROOT/PlatariumCore"
export PATH="$PATH:$CORE_DIR/target/release"

echo "=== Platarium Test Network ==="
echo "Gateway dir: $GATEWAY_DIR"
echo "Core dir:    $CORE_DIR"

# 1) Build Platarium Core (release)
if [ -d "$CORE_DIR" ]; then
  echo "[1/4] Building Platarium Core (release)..."
  (cd "$CORE_DIR" && cargo build --release 2>/dev/null) || {
    echo "WARN: Could not build PlatariumCore (cargo not found or build failed). Tests requiring Core will be skipped."
  }
  export PLATARIUM_CLI_PATH="$CORE_DIR/target/release/platarium-cli"
  [ -x "$PLATARIUM_CLI_PATH" ] || unset PLATARIUM_CLI_PATH
else
  echo "WARN: PlatariumCore not found at $CORE_DIR. Tests requiring Core will be skipped."
fi

# 2) Unit tests for Core integration (verify signature, etc.)
echo "[2/4] Running Core unit tests..."
(cd "$GATEWAY_DIR" && go test -v ./internal/core/... -count=1) || true

# 3) Start gateway in testnet mode in background
echo "[3/4] Starting gateway in testnet mode (port 2812, ws 2813)..."
(cd "$GATEWAY_DIR" && go run . -testnet -port 2812 -ws 2813) &
GWPID=$!
cleanup() { kill $GWPID 2>/dev/null || true; }
trap cleanup EXIT
sleep 2
if ! kill -0 $GWPID 2>/dev/null; then
  echo "WARN: Gateway exited (maybe Core not found). Skip integration tests."
else
  # 4) Integration tests (send TX to testnet)
  echo "[4/4] Running testnet integration tests..."
  (cd "$GATEWAY_DIR" && go test -tags=integration -v -run TestTestnet -count=1 .) || true
fi

echo "=== Test network run finished ==="
