#!/usr/bin/env bash
# Legacy entrypoint — now delegates to split node0/node1 layout.
# Prefer: bash update-and-restart.sh   (after code change)
#      or: bash restart-nodes.sh       (binary already deployed)
set -euo pipefail
cd "$(dirname "$0")/.."
exec bash restart-nodes.sh
