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

echo "[1/6] Creating service user '$SERVICE_USER'..."
if ! id "$SERVICE_USER" &>/dev/null; then
  useradd -r -s /bin/false -d "$DATA_DIR" "$SERVICE_USER"
  echo "[+] Service user '$SERVICE_USER' created."
else
  echo "[i] User '$SERVICE_USER' already exists."
fi

# Add neon to bluetooth & dialout groups if available
for grp in bluetooth dialout; do
  if getent group "$grp" &>/dev/null; then
    usermod -a -G "$grp" "$SERVICE_USER" || true
  fi
done

echo "[2/6] Enabling persistent lingering (services run even when logged out)..."
for u in "ubuntu" "$SERVICE_USER" "${SUDO_USER:-}"; do
  if [ -n "$u" ] && id "$u" &>/dev/null; then
    loginctl enable-linger "$u"
    echo "  [+] Enabled linger for $u"
  fi
done

echo "[3/6] Creating directories..."
mkdir -p "$INSTALL_DIR/python"
mkdir -p "$INSTALL_DIR/scripts"
mkdir -p "$DATA_DIR"
mkdir -p "$NAS_MOUNT_DIR"

echo "[4/6] Setting file permissions..."
chown -R "$SERVICE_USER:$SERVICE_GROUP" "$INSTALL_DIR"
chown -R "$SERVICE_USER:$SERVICE_GROUP" "$DATA_DIR"
chmod 750 "$DATA_DIR"

# Allow neon user to write to NAS mount
if [ -d "$NAS_MOUNT_DIR" ]; then
  chown -R "$SERVICE_USER:$SERVICE_GROUP" "$NAS_MOUNT_DIR" || true
  chmod -R 775 "$NAS_MOUNT_DIR" || true
fi

echo "[5/6] Installing and enabling all systemd units..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSTEMD_SRC="$SCRIPT_DIR/systemd"

if [ -d "$SYSTEMD_SRC" ]; then
  for svc in "$SYSTEMD_SRC"/*.service; do
    if [ -f "$svc" ]; then
      svc_name=$(basename "$svc")
      cp "$svc" "/etc/systemd/system/$svc_name"
      systemctl enable "$svc_name"
      echo "  [+] Installed and enabled /etc/systemd/system/$svc_name"
    fi
  done
  systemctl daemon-reload
fi

echo "[6/6] Setup complete!"
echo "All NeonServices will auto-start at boot and persist across logouts."
echo "Next steps:"
echo " 1. Ensure StationPC PocketCloud NAS is mounted at $NAS_MOUNT_DIR"
echo " 2. Deploy binary and companion scripts via deploy/deploy-to-host.sh"
echo " 3. Start services: sudo systemctl start neonservices"
echo "=========================================="
