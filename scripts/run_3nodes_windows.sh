#!/usr/bin/env bash
# Prints commands to start 3 nodes in 3 separate terminals (real mode).
# Does not build — instructions only.
#
# Usage: ./scripts/run_3nodes_windows.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$GATEWAY_DIR"

echo "=== Real mode: 3 nodes in 3 terminals ==="
echo ""
echo "Order: Terminal 1 first, then Terminal 2, then Terminal 3."
echo ""

echo "--- Terminal 1 (node 0, REST 2812, WS 2813) ---"
echo "cd $GATEWAY_DIR"
echo "mkdir -p data"
echo "export PLATARIUM_CLI_PATH=../PlatariumCore/target/release/platarium-cli"
echo "export PLATARIUM_STATE_FILE=data/state-node0.json"
echo "./platarium-gateway -testnet -port 2812 -ws 2813 -state-file \$PLATARIUM_STATE_FILE"
echo ""

echo "--- Terminal 2 (node 1, REST 2822, WS 2823) - start after node 0 ---"
echo "cd $GATEWAY_DIR"
echo "export PLATARIUM_CLI_PATH=../PlatariumCore/target/release/platarium-cli"
echo "export PLATARIUM_STATE_FILE=data/state-node1.json"
echo "PEERS='[\"ws://localhost:2813\"]' ./platarium-gateway -testnet -port 2822 -ws 2823 -state-file \$PLATARIUM_STATE_FILE"
echo ""

echo "--- Terminal 3 (node 2, REST 2832, WS 2833) - start after node 1 ---"
echo "cd $GATEWAY_DIR"
echo "export PLATARIUM_CLI_PATH=../PlatariumCore/target/release/platarium-cli"
echo "export PLATARIUM_STATE_FILE=data/state-node2.json"
echo "PEERS='[\"ws://localhost:2813\",\"ws://localhost:2823\"]' ./platarium-gateway -testnet -port 2832 -ws 2833 -state-file \$PLATARIUM_STATE_FILE"
echo ""

echo "After startup: API at localhost:2812, 2822, 2832; viz at http://localhost:2812/web/nodes-viz.html"
echo "Ensure Core is built: cd ../PlatariumCore && cargo build --release"
