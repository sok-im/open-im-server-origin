#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_FILE="_output/logs/openim.log"

cd "$ROOT_DIR"

echo "[1/4] Fetch latest code..."
git fetch --all --prune
git pull

echo "[2/4] Run mage..."
mage

echo "[3/4] Stop services..."
mage stop

echo "[4/4] Start services in background..."
mkdir -p "$(dirname "$LOG_FILE")"
nohup mage start > "$LOG_FILE" 2>&1 &

echo "Done. Logs: $ROOT_DIR/$LOG_FILE"
