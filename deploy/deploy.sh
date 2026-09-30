#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# If arguments were supplied, delegate directly to deploy-to-host.sh
if [ $# -gt 0 ]; then
  exec "$SCRIPT_DIR/deploy-to-host.sh" "$@"
fi

# Fallback to environment variables or defaults
TARGET="${NEON_DEPLOY_HOST:-192.168.1.100}"
USER="${NEON_DEPLOY_USER:-ubuntu}"
PORT="${NEON_DEPLOY_PORT:-22}"
KEY="${NEON_DEPLOY_KEY:-}"
MOUNT="${NEON_NAS_MOUNT:-/mnt/pocketcloud}"
DEVICE="${NEON_NAS_DEVICE:-}"

ARGS=("$TARGET" -u "$USER" -p "$PORT" -m "$MOUNT")
if [ -n "$KEY" ]; then
  ARGS+=(-i "$KEY")
fi
if [ -n "$DEVICE" ]; then
  ARGS+=(-d "$DEVICE")
fi

exec "$SCRIPT_DIR/deploy-to-host.sh" "${ARGS[@]}"
