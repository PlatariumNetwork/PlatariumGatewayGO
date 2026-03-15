#!/usr/bin/env bash
# Показує команди для запуску 3 нод у 3 окремих вікнах (реальний режим).
# Збірка не виконується - тільки вивід інструкцій.
#
# Usage: ./scripts/run_3nodes_windows.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$GATEWAY_DIR"

echo "=== Реальний режим: 3 ноди в 3 вікнах ==="
echo ""
echo "Порядок: спочатку Вікно 1, потім Вікно 2, потім Вікно 3."
echo ""

echo "--- Вікно 1 (нода 0, REST 2812, WS 2813) ---"
echo "cd $GATEWAY_DIR"
echo "./platarium-gateway -testnet -port 2812 -ws 2813"
echo ""

echo "--- Вікно 2 (нода 1, REST 2822, WS 2823) - запустити після ноди 0 ---"
echo "cd $GATEWAY_DIR"
echo "PEERS='[\"ws://localhost:2813\"]' ./platarium-gateway -testnet -port 2822 -ws 2823"
echo ""

echo "--- Вікно 3 (нода 2, REST 2832, WS 2833) - запустити після ноди 1 ---"
echo "cd $GATEWAY_DIR"
echo "PEERS='[\"ws://localhost:2813\",\"ws://localhost:2823\"]' ./platarium-gateway -testnet -port 2832 -ws 2833"
echo ""

echo "Після запуску: API - localhost:2812, 2822, 2832; візуалізація - http://localhost:2812/web/nodes-viz.html"
echo "Переконайтесь, що Core зібрано: cd ../PlatariumCore && cargo build --release"
