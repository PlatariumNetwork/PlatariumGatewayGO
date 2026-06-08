#!/usr/bin/env bash
# Prints commands to start 3 nodes in 3 separate terminals (real mode, multi-node L1/L2).
# Does not build — instructions only.
#
# Usage: ./scripts/run_3nodes.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CORE_DIR="$(cd "$GATEWAY_DIR/../PlatariumCore" && pwd)"
CLI="$CORE_DIR/target/release/platarium-cli"
DATA_DIR="$GATEWAY_DIR/data"

cd "$GATEWAY_DIR"
mkdir -p "$DATA_DIR"

echo "=== Real mode: 3 nodes in 3 terminals ==="
echo ""
echo "Prerequisites:"
echo "  cd $CORE_DIR && unset CARGO_TARGET_DIR && cargo build --release"
echo "  cd $GATEWAY_DIR && go build -o platarium-gateway ."
echo ""
echo "Each node uses its own Core state file (required for multi-node)."
echo "Order: Terminal 1 first, then Terminal 2, then Terminal 3."
echo ""

echo "--- Terminal 1 (node 0, REST 2812, WS 2813) ---"
echo "cd $GATEWAY_DIR"
echo "export PLATARIUM_CLI_PATH=$CLI"
echo "export PLATARIUM_STATE_FILE=$DATA_DIR/state-node0.json"
echo "./platarium-gateway -testnet -port 2812 -ws 2813 -state-file \$PLATARIUM_STATE_FILE"
echo ""

echo "--- Terminal 2 (node 1, REST 2822, WS 2823) - start after node 0 ---"
echo "cd $GATEWAY_DIR"
echo "export PLATARIUM_CLI_PATH=$CLI"
echo "export PLATARIUM_STATE_FILE=$DATA_DIR/state-node1.json"
echo "PEERS='[\"ws://localhost:2813\"]' ./platarium-gateway -testnet -port 2822 -ws 2823 -state-file \$PLATARIUM_STATE_FILE"
echo ""

echo "--- Terminal 3 (node 2, REST 2832, WS 2833) - start after node 1 ---"
echo "cd $GATEWAY_DIR"
echo "export PLATARIUM_CLI_PATH=$CLI"
echo "export PLATARIUM_STATE_FILE=$DATA_DIR/state-node2.json"
echo "PEERS='[\"ws://localhost:2813\",\"ws://localhost:2823\"]' ./platarium-gateway -testnet -port 2832 -ws 2833 -state-file \$PLATARIUM_STATE_FILE"
echo ""

echo "After startup:"
echo "  API: localhost:2812, 2822, 2832"
echo "  Viz: http://localhost:2812/web/nodes-viz.html"
echo ""
echo "Quick test (on node 0):"
echo "  curl -X POST http://127.0.0.1:2812/api/faucet -H 'Content-Type: application/json' -d '{\"address\":\"PxAlice\"}'"
echo "  curl -X POST http://127.0.0.1:2812/api/l1-collect"
echo "  curl -X POST http://127.0.0.1:2812/api/l2-confirm"
echo "  curl http://127.0.0.1:2812/pg-bal/PxAlice"
echo "  curl http://127.0.0.1:2822/pg-bal/PxAlice   # should match after block_confirmed sync"
