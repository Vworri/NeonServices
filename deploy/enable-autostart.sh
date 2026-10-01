#!/usr/bin/env bash
# ==============================================================================
# NeonServices Ubuntu Persistent Auto-Start Configurator
# Enables all services to auto-start on boot and persist after user logout.
# ==============================================================================
set -euo pipefail

echo "=================================================================="
echo "  NeonServices Ubuntu Auto-Start & Persistent Background Setup    "
echo "=================================================================="

if [ "$EUID" -ne 0 ]; then
  echo "[-] Please run as root or with sudo:"
  echo "    sudo ./enable-autostart.sh"
  exit 1
fi

INSTALL_DIR="/opt/neonservices"
SYSTEMD_DIR="/etc/systemd/system"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 1. Enable Systemd Lingering for Users
echo "[1/4] Enabling systemd lingering (services keep running on logout)..."
USERS_TO_LINGER=("ubuntu" "neon")
if [ -n "${SUDO_USER:-}" ] && [ "${SUDO_USER}" != "root" ]; then
  USERS_TO_LINGER+=("${SUDO_USER}")
fi

for u in "${USERS_TO_LINGER[@]}"; do
  if id "$u" &>/dev/null; then
    loginctl enable-linger "$u"
    echo "  [+] Lingering ENABLED for user: $u"
  fi
done

# 2. Search for Systemd Unit Files
echo "[2/4] Locating and installing NeonServices systemd units..."
CANDIDATE_DIRS=(
  "$SCRIPT_DIR/systemd"
  "$SCRIPT_DIR"
  "$INSTALL_DIR/deploy/systemd"
  "$INSTALL_DIR/systemd"
)

FOUND_UNITS=()
for dir in "${CANDIDATE_DIRS[@]}"; do
  if [ -d "$dir" ]; then
    for f in "$dir"/neon*.service; do
      if [ -f "$f" ]; then
        unit_name=$(basename "$f")
        cp "$f" "$SYSTEMD_DIR/$unit_name"
        echo "  [+] Installed $unit_name -> $SYSTEMD_DIR/$unit_name"
        FOUND_UNITS+=("$unit_name")
      fi
    done
    if [ ${#FOUND_UNITS[@]} -gt 0 ]; then
      break
    fi
  fi
done

if [ ${#FOUND_UNITS[@]} -eq 0 ]; then
  echo "[!] No unit files found in candidate paths. Installing standard units..."
  
  # Standard neonservices.service
  cat << 'UNIT_EOF' > "$SYSTEMD_DIR/neonservices.service"
[Unit]
Description=NeonServices Multi-User Backend & StationPC PocketCloud NAS Gateway
After=network.target local-fs.target remote-fs.target
Wants=network-online.target

[Service]
Type=simple
User=neon
Group=neon
WorkingDirectory=/opt/neonservices
ExecStartPre=+/bin/mkdir -p /var/lib/neonservices /mnt/pocketcloud/storage
ExecStartPre=+/bin/chown -R neon:neon /var/lib/neonservices /mnt/pocketcloud/storage
ExecStart=/opt/neonservices/api-server -config /opt/neonservices/config.yaml
Restart=always
RestartSec=5s
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT_EOF
  echo "  [+] Generated $SYSTEMD_DIR/neonservices.service"
  FOUND_UNITS+=("neonservices.service")

  # Standard neon-nas-worker.service
  cat << 'UNIT_EOF' > "$SYSTEMD_DIR/neon-nas-worker.service"
[Unit]
Description=NeonServices Python NAS Storage Worker
After=network.target neonservices.service
Wants=neonservices.service

[Service]
Type=simple
User=neon
Group=neon
WorkingDirectory=/opt/neonservices/python
ExecStart=/usr/bin/python3 -u /opt/neonservices/python/nas_worker.py
Restart=always
RestartSec=5s
Environment=NEON_API_URL=http://127.0.0.1:8080
Environment=PYTHONUNBUFFERED=1

[Install]
WantedBy=multi-user.target
UNIT_EOF
  echo "  [+] Generated $SYSTEMD_DIR/neon-nas-worker.service"
  FOUND_UNITS+=("neon-nas-worker.service")

  # Standard neon-opendisplay.service
  cat << 'UNIT_EOF' > "$SYSTEMD_DIR/neon-opendisplay.service"
[Unit]
Description=NeonServices OpenDisplay E-Paper BLE Auto-Pusher
After=network.target bluetooth.target neonservices.service
Wants=bluetooth.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/neonservices
ExecStart=/usr/bin/python3 -u /opt/neonservices/scripts/opendisplay_pusher.py --mac AC:27:6E:A6:AA:F5 --url http://127.0.0.1:8080/screen --interval 60
Restart=always
RestartSec=10s
Environment=PYTHONUNBUFFERED=1

[Install]
WantedBy=multi-user.target
UNIT_EOF
  echo "  [+] Generated $SYSTEMD_DIR/neon-opendisplay.service"
  FOUND_UNITS+=("neon-opendisplay.service")
fi

# 3. Reload daemon and enable units for system boot
echo "[3/4] Enabling services for system-level auto-start on boot..."
systemctl daemon-reload

for unit in "${FOUND_UNITS[@]}"; do
  systemctl enable "$unit"
  echo "  [+] Enabled $unit (will auto-start on boot and survive logout)"
  # Start or restart
  systemctl restart "$unit" || true
done

# 4. Verification
echo "[4/4] Verifying running services status..."
echo "------------------------------------------------------------------"
for unit in "${FOUND_UNITS[@]}"; do
  status=$(systemctl is-active "$unit" 2>/dev/null || echo "inactive")
  echo "  * $unit : $status"
done
echo "------------------------------------------------------------------"
echo "🎉 Setup complete! All services will auto-start at boot and persist"
echo "   continuously even after you disconnect or log out of Ubuntu."
echo "=================================================================="
