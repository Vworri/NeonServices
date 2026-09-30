#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# NeonServices Remote SSH Deployment & StationPC PocketCloud NAS Provisioner
# ==============================================================================

# Default parameters
SSH_USER="ubuntu"
SSH_PORT="22"
SSH_KEY=""
NAS_MOUNT_POINT="/mnt/pocketcloud"
NAS_DEVICE="" # Optional, e.g. /dev/sdb1 or /dev/nvme0n1p1
REMOTE_INSTALL_DIR="/opt/neonservices"
FORCE_FORMAT=false
REMOTE_HOST=""

usage() {
  cat << 'HELP'
Usage:
  ./deploy/deploy-to-host.sh <IP_or_USER@IP> [options]

Arguments:
  <IP_or_USER@IP>         Target Ubuntu host (e.g., 192.168.1.100 or ubuntu@192.168.1.100)

Options:
  -u, --user <username>   SSH username (default: ubuntu)
  -p, --port <port>       SSH port (default: 22)
  -i, --key <key_path>    SSH private key (default: ~/.ssh/id_ed25519 or ~/.ssh/id_rsa)
  -m, --mount <path>      NAS mount point (default: /mnt/pocketcloud)
  -d, --device <dev>      NAS block device/partition to mount (e.g., /dev/sdb1)
  --format                Format the NAS device to ext4 if it has no filesystem
  -h, --help              Show this help message

Examples:
  ./deploy/deploy-to-host.sh 192.168.1.100
  ./deploy/deploy-to-host.sh ubuntu@192.168.1.100 -i ~/.ssh/my_key
  ./deploy/deploy-to-host.sh 192.168.1.100 -d /dev/sdb1 -m /mnt/pocketcloud
HELP
  exit 0
}

if [ $# -lt 1 ]; then
  usage
fi

# Check for help flag first
for arg in "$@"; do
  if [ "$arg" = "-h" ] || [ "$arg" = "--help" ]; then
    usage
  fi
done

# First non-flag argument is target
while [[ $# -gt 0 ]]; do
  case "$1" in
    -u|--user)
      SSH_USER="$2"
      shift 2
      ;;
    -p|--port)
      SSH_PORT="$2"
      shift 2
      ;;
    -i|--key)
      SSH_KEY="$2"
      shift 2
      ;;
    -m|--mount)
      NAS_MOUNT_POINT="$2"
      shift 2
      ;;
    -d|--device)
      NAS_DEVICE="$2"
      shift 2
      ;;
    --format)
      FORCE_FORMAT=true
      shift
      ;;
    -*)
      echo "[-] Unknown option: $1"
      usage
      ;;
    *)
      if [ -z "$REMOTE_HOST" ]; then
        TARGET="$1"
        if [[ "$TARGET" =~ ^([^@]+)@(.+)$ ]]; then
          SSH_USER="${BASH_REMATCH[1]}"
          REMOTE_HOST="${BASH_REMATCH[2]}"
        else
          REMOTE_HOST="$TARGET"
        fi
      else
        echo "[-] Unexpected extra argument: $1"
        usage
      fi
      shift
      ;;
  esac
done

if [ -z "$REMOTE_HOST" ]; then
  echo "[-] Error: Missing target host IP or user@IP."
  usage
fi

# Resolve SSH Key if not provided
if [ -z "$SSH_KEY" ]; then
  if [ -f "$HOME/.ssh/id_ed25519" ]; then
    SSH_KEY="$HOME/.ssh/id_ed25519"
  elif [ -f "$HOME/.ssh/id_rsa" ]; then
    SSH_KEY="$HOME/.ssh/id_rsa"
  fi
fi

SSH_OPTS=(-p "$SSH_PORT" -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)
if [ -n "$SSH_KEY" ]; then
  SSH_OPTS+=(-i "$SSH_KEY")
fi

run_ssh() {
  ssh "${SSH_OPTS[@]}" "$SSH_USER@$REMOTE_HOST" "$@"
}

run_scp() {
  scp -P "$SSH_PORT" ${SSH_KEY:+-i "$SSH_KEY"} -o StrictHostKeyChecking=accept-new "$@"
}

echo "=================================================================="
echo " 🚀 NeonServices Deployment to $SSH_USER@$REMOTE_HOST:$SSH_PORT"
echo "=================================================================="

# 1. Test SSH Connection
echo "[1/7] Testing SSH connectivity..."
if ! run_ssh "echo 'Connected successfully to \$(hostname) (\$(uname -srm))'" ; then
  echo "[-] Failed to connect to $SSH_USER@$REMOTE_HOST. Check IP, credentials, and SSH keys."
  exit 1
fi

# 2. Detect Remote Architecture
echo "[2/7] Detecting remote CPU architecture..."
REMOTE_ARCH=$(run_ssh "uname -m")
case "$REMOTE_ARCH" in
  x86_64)
    GOARCH="amd64"
    ;;
  aarch64|arm64)
    GOARCH="arm64"
    ;;
  armv7l)
    GOARCH="arm"
    ;;
  *)
    echo "[-] Unsupported remote architecture: $REMOTE_ARCH"
    exit 1
    ;;
esac
echo "[+] Target architecture: linux/$GOARCH"

# 3. Verify & Provision NAS Mount
echo "[3/7] Verifying StationPC PocketCloud NAS mount at $NAS_MOUNT_POINT..."

REMOTE_MOUNT_SCRIPT=$(cat << REMOTE_EOF
set -euo pipefail

MOUNT_POINT="$NAS_MOUNT_POINT"
DEVICE="$NAS_DEVICE"
FORCE_FMT="$FORCE_FORMAT"
STORAGE_DIR="\$MOUNT_POINT/storage"

echo "[i] Checking if \$MOUNT_POINT is mounted..."
if findmnt -M "\$MOUNT_POINT" >/dev/null 2>&1; then
  echo "[+] \$MOUNT_POINT is already mounted:"
  findmnt -M "\$MOUNT_POINT" -o SOURCE,FSTYPE,SIZE,USED,AVAIL,TARGET
else
  echo "[!] \$MOUNT_POINT is NOT currently mounted."

  # If device not passed, attempt discovery of unmounted candidate drives
  if [ -z "\$DEVICE" ]; then
    echo "[i] Searching for available unmounted disks/partitions..."
    CANDIDATE=\$(lsblk -rpno NAME,TYPE,MOUNTPOINT,FSTYPE,SIZE,MODEL | awk '\$2=="part" && \$3=="" {print \$1; exit}')
    if [ -n "\$CANDIDATE" ]; then
      DEVICE="\$CANDIDATE"
      echo "[i] Auto-detected candidate unmounted partition: \$DEVICE"
    else
      echo "[-] No unmounted partition auto-detected."
      echo "    Available block devices on host:"
      lsblk -o NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT,MODEL,VENDOR
      echo "[-] Please specify device using: -d /dev/sdX1"
      exit 1
    fi
  fi

  echo "[i] Inspecting block device \$DEVICE..."
  if ! [ -b "\$DEVICE" ]; then
    echo "[-] Device \$DEVICE does not exist!"
    exit 1
  fi

  CURRENT_FS=\$(blkid -s TYPE -o value "\$DEVICE" || true)
  if [ -z "\$CURRENT_FS" ]; then
    if [ "\$FORCE_FMT" = "true" ]; then
      echo "[!] Formatting \$DEVICE to ext4..."
      sudo mkfs.ext4 -F -L "PocketCloud" "\$DEVICE"
    else
      echo "[-] Device \$DEVICE has no filesystem. Re-run with --format to format as ext4."
      exit 1
    fi
  else
    echo "[+] Found filesystem '\$CURRENT_FS' on \$DEVICE."
  fi

  # Create mount directory
  sudo mkdir -p "\$MOUNT_POINT"

  # Mount device
  echo "[+] Mounting \$DEVICE to \$MOUNT_POINT..."
  sudo mount "\$DEVICE" "\$MOUNT_POINT"

  # Add to /etc/fstab if not already present (using nofail so server reboots safely)
  DEV_UUID=\$(blkid -s UUID -o value "\$DEVICE" || true)
  if [ -n "\$DEV_UUID" ]; then
    if ! grep -q "\$DEV_UUID" /etc/fstab; then
      echo "[+] Adding persistent mount to /etc/fstab (UUID=\$DEV_UUID)..."
      echo "UUID=\$DEV_UUID \$MOUNT_POINT ext4 defaults,noatime,nofail 0 2" | sudo tee -a /etc/fstab
    fi
  fi
fi

# Ensure storage directory exists and has correct permissions
sudo mkdir -p "\$STORAGE_DIR"

# Ensure neon user exists
if ! id "neon" &>/dev/null; then
  echo "[+] Creating system service user 'neon'..."
  sudo useradd -r -s /bin/false -d /var/lib/neonservices neon || true
fi

# Set ownership and permissions for neon service
sudo chown -R neon:neon "\$STORAGE_DIR"
sudo chmod 775 "\$STORAGE_DIR"
echo "[+] NAS storage directory ready at \$STORAGE_DIR"
REMOTE_EOF
)

run_ssh "bash -s" <<< "$REMOTE_MOUNT_SCRIPT"

# 4. Compile Standalone Go Binaries Locally
echo "[4/7] Compiling static binaries for linux/$GOARCH..."
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -ldflags="-s -w" -o "bin/api-server-linux-$GOARCH" ./cmd/api-server
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -ldflags="-s -w" -o "bin/neon-ctl-linux-$GOARCH" ./cmd/neon-ctl
echo "[+] Built: bin/api-server-linux-$GOARCH and bin/neon-ctl-linux-$GOARCH"

# 5. Prepare Remote Installation Directory and Transfer Files
echo "[5/7] Deploying binaries to $REMOTE_HOST:$REMOTE_INSTALL_DIR..."
run_ssh "sudo mkdir -p $REMOTE_INSTALL_DIR /var/lib/neonservices && sudo chown -R $SSH_USER:$SSH_USER $REMOTE_INSTALL_DIR"

run_scp "bin/api-server-linux-$GOARCH" "$SSH_USER@$REMOTE_HOST:$REMOTE_INSTALL_DIR/api-server.new"
run_scp "bin/neon-ctl-linux-$GOARCH" "$SSH_USER@$REMOTE_HOST:$REMOTE_INSTALL_DIR/neon-ctl.new"

run_ssh "chmod +x $REMOTE_INSTALL_DIR/api-server.new $REMOTE_INSTALL_DIR/neon-ctl.new && \
         mv $REMOTE_INSTALL_DIR/api-server.new $REMOTE_INSTALL_DIR/api-server && \
         mv $REMOTE_INSTALL_DIR/neon-ctl.new $REMOTE_INSTALL_DIR/neon-ctl && \
         sudo ln -sf $REMOTE_INSTALL_DIR/neon-ctl /usr/local/bin/neon-ctl || true"

# 6. Setup Configuration and Secrets on Remote
echo "[6/7] Checking remote configuration and secrets..."
CONFIG_SETUP_SCRIPT=$(cat << REMOTE_CONF_EOF
set -euo pipefail
DIR="$REMOTE_INSTALL_DIR"
MOUNT="$NAS_MOUNT_POINT/storage"

if [ ! -f "\$DIR/config.yaml" ]; then
  echo "[+] Generating initial production config.yaml..."
  
  # Generate a 256-bit secure secret
  JWT_SECRET=\$(head -c 32 /dev/urandom | xxd -p -c 32 2>/dev/null || openssl rand -hex 32 2>/dev/null || tr -dc 'a-zA-Z0-9' </dev/urandom | head -c 64)
  
  cat << YAML_EOF > "\$DIR/config.yaml"
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 15s
  write_timeout: 30s
  shutdown_timeout: 10s
  env: "production"

auth:
  jwt_secret: "\$JWT_SECRET"
  token_ttl: 24h
  allow_registration: true

database:
  path: "/var/lib/neonservices/neonservices.db"

storage:
  base_mount_path: "\$MOUNT"
  default_quota_bytes: 53687091200
  max_upload_size_mb: 2048
  check_mount_available: true
YAML_EOF

  echo "[+] Generated new secure JWT secret in \$DIR/config.yaml"
else
  echo "[i] Existing config found at \$DIR/config.yaml. Preserving secrets."
fi

# Ensure neon owns directories and config has restricted 0600 permissions
sudo chown -R neon:neon "$REMOTE_INSTALL_DIR" "/var/lib/neonservices"
sudo chmod 600 "\$DIR/config.yaml"
sudo chmod 750 "$REMOTE_INSTALL_DIR" "/var/lib/neonservices"
REMOTE_CONF_EOF
)

run_ssh "bash -s" <<< "$CONFIG_SETUP_SCRIPT"

# 7. Install Systemd Service and Restart
echo "[7/7] Installing systemd unit and starting NeonServices..."
run_scp "deploy/systemd/neonservices.service" "$SSH_USER@$REMOTE_HOST:/tmp/neonservices.service"

run_ssh "sudo cp /tmp/neonservices.service /etc/systemd/system/neonservices.service && \
         sudo systemctl daemon-reload && \
         sudo systemctl enable neonservices && \
         sudo systemctl restart neonservices"

# Health Check Verification
echo ""
echo "[*] Waiting for service to report active status..."
sleep 2

STATUS_OUTPUT=$(run_ssh "systemctl is-active neonservices || true")
if [ "$STATUS_OUTPUT" != "active" ]; then
  echo "[-] Service failed to start! Fetching last 20 log lines:"
  run_ssh "journalctl -u neonservices -n 20 --no-pager"
  exit 1
fi

echo "[+] Service is ACTIVE (running)!"
echo ""
echo "[*] Querying local REST API health endpoint on host..."
HEALTH_CHECK=$(run_ssh "curl -s http://127.0.0.1:8080/api/v1/health || true")
echo "    Response: $HEALTH_CHECK"

echo ""
echo "=================================================================="
echo " 🎉 Deployment and NAS Provisioning Complete!"
echo "=================================================================="
echo " - Remote API Base:    http://$REMOTE_HOST:8080/api/v1"
echo " - NAS Storage Path:   $NAS_MOUNT_POINT/storage"
echo " - Systemd Service:    neonservices.service"
echo " - CLI on Host:        neon-ctl (or /opt/neonservices/neon-ctl)"
echo ""
echo "Quick Commands:"
echo "  Check Logs:          ssh $SSH_USER@$REMOTE_HOST 'journalctl -u neonservices -f'"
echo "  Check NAS Status:    ssh $SSH_USER@$REMOTE_HOST 'neon-ctl nas-status'"
echo "  Control via Fyne:    go run ./cmd/fyne-manager (Connect to $REMOTE_HOST)"
echo "=================================================================="
