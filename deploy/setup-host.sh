#!/usr/bin/env bash
set -euo pipefail

echo "=========================================="
echo " NeonServices Ubuntu Host Initial Setup   "
echo "=========================================="

if [ "$EUID" -ne 0 ]; then
  echo "[-] Please run as root or with sudo:"
  echo "    sudo ./setup-host.sh"
  exit 1
fi

SERVICE_USER="neon"
SERVICE_GROUP="neon"
INSTALL_DIR="/opt/neonservices"
DATA_DIR="/var/lib/neonservices"
NAS_MOUNT_DIR="/mnt/pocketcloud/storage"

echo "[1/5] Creating service user '$SERVICE_USER'..."
if ! id "$SERVICE_USER" &>/dev/null; then
  useradd -r -s /bin/false -d "$DATA_DIR" "$SERVICE_USER"
  echo "[+] Service user '$SERVICE_USER' created."
else
  echo "[i] User '$SERVICE_USER' already exists."
fi

echo "[2/5] Creating directories..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$NAS_MOUNT_DIR"

echo "[3/5] Setting file permissions..."
chown -R "$SERVICE_USER:$SERVICE_GROUP" "$INSTALL_DIR"
chown -R "$SERVICE_USER:$SERVICE_GROUP" "$DATA_DIR"
chmod 750 "$DATA_DIR"

# Allow neon user to write to NAS mount
if [ -d "$NAS_MOUNT_DIR" ]; then
  chown -R "$SERVICE_USER:$SERVICE_GROUP" "$NAS_MOUNT_DIR" || true
  chmod -R 775 "$NAS_MOUNT_DIR" || true
fi

echo "[4/5] Installing systemd unit..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/systemd/neonservices.service" ]; then
  cp "$SCRIPT_DIR/systemd/neonservices.service" /etc/systemd/system/neonservices.service
  systemctl daemon-reload
  echo "[+] Systemd service installed at /etc/systemd/system/neonservices.service"
fi

echo "[5/5] Setup complete!"
echo "Next steps:"
echo " 1. Ensure StationPC PocketCloud NAS is mounted at $NAS_MOUNT_DIR (or edit /opt/neonservices/config.yaml)"
echo " 2. Deploy binary and config using 'make deploy' or deploy/deploy.sh"
echo " 3. Start service: sudo systemctl start neonservices"
echo "=========================================="
