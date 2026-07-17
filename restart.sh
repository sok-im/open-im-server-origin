#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_FILE="_output/logs/openim.log"

cd "$ROOT_DIR"

echo "[1/5] Fetch latest code..."
git fetch --all --prune
git pull

echo "[2/5] Update protocol (discard local changes, then pull)..."
# `mage` regenerates protocol/*/*.pb.go on every build (go.mod: replace => ./protocol),
# which dirties the submodule working tree and would otherwise block `git pull`.
# Reset to a clean tree first so the pull always succeeds without manual deletion.
# NOTE: this DISCARDS all local changes under ./protocol — commit & push any proto
# edits you want to keep to the protocol repo before running this.
(
  cd protocol
  git reset --hard
  git clean -fd
  git pull
)

echo "[3/5] Run mage..."
mage

echo "[4/5] Stop services..."
mage stop

echo "[5/5] Start services in background..."
mkdir -p "$(dirname "$LOG_FILE")"
nohup mage start > "$LOG_FILE" 2>&1 &

echo "Done. Logs: $ROOT_DIR/$LOG_FILE"
