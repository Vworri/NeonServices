#!/usr/bin/env bash
set -euo pipefail

# Configuration defaults (can be overridden by environment variables)
REMOTE_HOST="${NEON_DEPLOY_HOST:-192.168.1.100}"
REMOTE_USER="${NEON_DEPLOY_USER:-ubuntu}"
REMOTE_PORT="${NEON_DEPLOY_PORT:-22}"
SSH_KEY="${NEON_DEPLOY_KEY:-$HOME/.ssh/id_ed25519}"
REMOTE_DIR="${NEON_DEPLOY_DIR:-/opt/neonservices}"
GOARCH="${NEON_TARGET_ARCH:-amd64}" # amd64 or arm64

# Check SSH key fallback
if [ ! -f "$SSH_KEY" ] && [ -f "$HOME/.ssh/id_rsa" ]; then
  SSH_KEY="$HOME/.ssh/id_rsa"
fi

SSH_CMD="ssh -p $REMOTE_PORT -i $SSH_KEY $REMOTE_USER@$REMOTE_HOST"
SCP_CMD="scp -P $REMOTE_PORT -i $SSH_KEY"

echo "=========================================="
echo " NeonServices Automated SSH Deployment    "
echo " Target: $REMOTE_USER@$REMOTE_HOST:$REMOTE_PORT"
echo " Arch:   linux/$GOARCH"
echo "=========================================="

echo "[1/5] Building Go binary for linux/$GOARCH (CGO_ENABLED=0)..."
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -ldflags="-s -w" -o bin/api-server-linux-$GOARCH ./cmd/api-server
echo "[+] Build complete: bin/api-server-linux-$GOARCH"

echo "[2/5] Checking remote directory..."
$SSH_CMD "sudo mkdir -p $REMOTE_DIR && sudo chown -R $REMOTE_USER:$REMOTE_USER $REMOTE_DIR"

echo "[3/5] Transferring binary and configuration..."
$SCP_CMD "bin/api-server-linux-$GOARCH" "$REMOTE_USER@$REMOTE_HOST:$REMOTE_DIR/api-server.new"
$SSH_CMD "chmod +x $REMOTE_DIR/api-server.new && mv $REMOTE_DIR/api-server.new $REMOTE_DIR/api-server"

# Copy config if not already present on remote
if $SSH_CMD "[ ! -f $REMOTE_DIR/config.yaml ]"; then
  echo "[i] Remote config not found. Copying config.example.yaml..."
  $SCP_CMD "config.example.yaml" "$REMOTE_USER@$REMOTE_HOST:$REMOTE_DIR/config.yaml"
fi

echo "[4/5] Updating and reloading systemd unit..."
$SCP_CMD "deploy/systemd/neonservices.service" "$REMOTE_USER@$REMOTE_HOST:/tmp/neonservices.service"
$SSH_CMD "sudo cp /tmp/neonservices.service /etc/systemd/system/neonservices.service && sudo systemctl daemon-reload"

echo "[5/5] Restarting NeonServices..."
$SSH_CMD "sudo systemctl restart neonservices && sleep 2 && systemctl is-active neonservices"

echo "=========================================="
echo "[+] Deployment successful!"
echo "Check logs anytime with:"
echo "    ssh -i $SSH_KEY -p $REMOTE_PORT $REMOTE_USER@$REMOTE_HOST 'journalctl -u neonservices -f'"
echo "Or using neon-ctl:"
echo "    ./bin/neon-ctl logs"
echo "=========================================="
