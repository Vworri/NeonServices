#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# NeonServices Remote SSH Deployment & StationPC PocketCloud NAS Provisioner
# Supports Block Devices (NVMe/SATA/USB) and Network Shares (SMB/CIFS/NFS)
# ==============================================================================

SSH_USER="ubuntu"
SSH_PORT="22"
SSH_KEY=""
NAS_MOUNT_POINT="/mnt/pocketcloud"
NAS_TARGET="" # Can be /dev/sdX1 or smb://host/share or //host/share
SMB_USER="guest"
SMB_PASS=""
REMOTE_INSTALL_DIR="/opt/neonservices"
FORCE_FORMAT=false
SKIP_MOUNT=false
REMOTE_HOST=""

usage() {
  cat << 'HELP'
Usage:
  ./deploy/deploy-to-host.sh <IP_or_USER@IP> [options]

Arguments:
  <IP_or_USER@IP>         Target Ubuntu host (e.g., lab@192.168.3.54)

Options:
  -u, --user <username>   SSH username (default: ubuntu)
  -p, --port <port>       SSH port (default: 22)
  -i, --key <key_path>    SSH private key (default: ~/.ssh/id_ed25519 or ~/.ssh/id_rsa)
  -m, --mount <path>      NAS mount point (default: /mnt/pocketcloud)
  -d, --device <target>   NAS target: block device (/dev/sdb1) or network share (smb://sp-4b51.local/data)
  --smb-user <user>       SMB share username (default: guest)
  --smb-pass <pass>       SMB share password (optional)
  --format                Format block device to ext4 if it has no filesystem
  --skip-mount            Skip mounting (use local directory or existing mount)
  -h, --help              Show this help message

Examples:
  # Deploy with StationPC PocketCloud SMB network share:
  ./deploy/deploy-to-host.sh lab@192.168.3.54 -d smb://sp-4b51.local/

  # Deploy with specific share name:
  ./deploy/deploy-to-host.sh lab@192.168.3.54 -d smb://sp-4b51.local/share

  # Deploy using local storage (skip mount):
  ./deploy/deploy-to-host.sh lab@192.168.3.54 --skip-mount
HELP
  exit 0
}

if [ $# -lt 1 ]; then
  usage
fi

for arg in "$@"; do
  if [ "$arg" = "-h" ] || [ "$arg" = "--help" ]; then
    usage
  fi
done

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
    -d|--device|--nas|--smb)
      NAS_TARGET="$2"
      shift 2
      ;;
    --smb-user)
      SMB_USER="$2"
      shift 2
      ;;
    --smb-pass)
      SMB_PASS="$2"
      shift 2
      ;;
    --format)
      FORCE_FORMAT=true
      shift
      ;;
    --skip-mount)
      SKIP_MOUNT=true
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

# Resolve SSH Key
if [ -z "$SSH_KEY" ]; then
  if [ -f "$HOME/.ssh/id_ed25519" ]; then
    SSH_KEY="$HOME/.ssh/id_ed25519"
  elif [ -f "$HOME/.ssh/id_rsa" ]; then
    SSH_KEY="$HOME/.ssh/id_rsa"
  fi
fi

# Create temporary directory for SSH ControlMaster multiplexing
SOCKET_DIR=$(mktemp -d /tmp/neon-ssh-XXXXXX)
SOCKET_PATH="$SOCKET_DIR/cm-%r@%h:%p"
cleanup() {
  ssh -O exit -S "$SOCKET_PATH" "$SSH_USER@$REMOTE_HOST" 2>/dev/null || true
  rm -rf "$SOCKET_DIR"
}
trap cleanup EXIT

SSH_BASE=(-p "$SSH_PORT" -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o ControlMaster=auto -o ControlPath="$SOCKET_PATH" -o ControlPersist=5m)
if [ -n "$SSH_KEY" ]; then
  SSH_BASE+=(-i "$SSH_KEY")
fi

run_ssh() {
  ssh "${SSH_BASE[@]}" "$SSH_USER@$REMOTE_HOST" "$@"
}

run_ssh_interactive() {
  ssh -tt "${SSH_BASE[@]}" "$SSH_USER@$REMOTE_HOST" "$@"
}

run_scp() {
  scp -P "$SSH_PORT" ${SSH_KEY:+-i "$SSH_KEY"} -o StrictHostKeyChecking=accept-new -o ControlPath="$SOCKET_PATH" "$@"
}

run_remote_script() {
  local script_content="$1"
  local tmp_local
  tmp_local=$(mktemp /tmp/neon-step-XXXXXX.sh)
  printf "%s\n" "$script_content" > "$tmp_local"
  run_scp "$tmp_local" "$SSH_USER@$REMOTE_HOST:/tmp/neon-step.sh"
  rm -f "$tmp_local"
  run_ssh_interactive "sudo bash /tmp/neon-step.sh; rm -f /tmp/neon-step.sh"
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
REMOTE_ARCH=$(run_ssh "uname -m" | tr -d '\r\n')
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

# 3. Verify & Provision NAS Mount (SMB / Block / Local)
echo "[3/7] Verifying StationPC PocketCloud NAS mount at $NAS_MOUNT_POINT..."

CLEAN_TARGET="$NAS_TARGET"
if [[ "$CLEAN_TARGET" =~ ^smb://(.*)$ ]]; then
  CLEAN_TARGET="//${BASH_REMATCH[1]}"
fi
if [[ "$CLEAN_TARGET" =~ ^cifs://(.*)$ ]]; then
  CLEAN_TARGET="//${BASH_REMATCH[1]}"
fi

STEP3_SCRIPT=$(cat << REMOTE_EOF
set -euo pipefail

MOUNT_POINT="$NAS_MOUNT_POINT"
TARGET="$CLEAN_TARGET"
SMB_USER="$SMB_USER"
SMB_PASS="$SMB_PASS"
FORCE_FMT="$FORCE_FORMAT"
SKIP="$SKIP_MOUNT"
STORAGE_DIR="\$MOUNT_POINT/storage"

# Create service user and base mount directory immediately
if ! id "neon" &>/dev/null; then
  echo "[+] Creating system service user 'neon'..."
  useradd -r -s /bin/false -d /var/lib/neonservices neon || true
fi
NEON_UID=\$(id -u neon)
NEON_GID=\$(id -g neon)

mkdir -p "\$MOUNT_POINT" "\$STORAGE_DIR" "/var/lib/neonservices"
chown -R neon:neon "\$MOUNT_POINT" "/var/lib/neonservices" 2>/dev/null || true
chmod 775 "\$MOUNT_POINT" "\$STORAGE_DIR" 2>/dev/null || true

if [ "\$SKIP" = "true" ]; then
  echo "[i] Using local folder at \$STORAGE_DIR (--skip-mount requested)."
elif findmnt -M "\$MOUNT_POINT" >/dev/null 2>&1; then
  echo "[+] \$MOUNT_POINT is already mounted:"
  findmnt -M "\$MOUNT_POINT" -o SOURCE,FSTYPE,SIZE,USED,AVAIL,TARGET || true
else
  echo "[!] \$MOUNT_POINT is not yet mounted."

  # Check if network share (starts with //)
  if [[ "\$TARGET" =~ ^//([^/]+)(/.*)?\$ ]]; then
    SMB_HOST="\${BASH_REMATCH[1]}"
    SMB_SHARE="\${BASH_REMATCH[2]}"
    SMB_SHARE="\${SMB_SHARE%/}"

    echo "[i] Network share detected: host '\$SMB_HOST', share '\$SMB_SHARE'"

    # Install mDNS resolver if using .local host
    if [[ "\$SMB_HOST" == *".local"* ]] && ! which avahi-resolve &>/dev/null; then
      echo "[i] Installing avahi-daemon for .local mDNS resolution..."
      apt-get update -qq && apt-get install -y -qq avahi-daemon avahi-utils || true
    fi

    # Install cifs-utils & smbclient if not installed
    if ! which mount.cifs &>/dev/null || ! which smbclient &>/dev/null; then
      echo "[i] Installing cifs-utils and smbclient..."
      apt-get update -qq && apt-get install -y -qq cifs-utils smbclient || true
    fi

    # If share name is empty, attempt discovery
    if [ -z "\$SMB_SHARE" ] || [ "\$SMB_SHARE" = "/" ]; then
      echo "[i] Discovering SMB shares on \$SMB_HOST..."
      
      DISCOVERED=\$(smbclient -L "smb://\$SMB_HOST" -N -g 2>/dev/null | awk -F'|' '\$1=="Disk" && \$2!~/\$$/ {print \$2; exit}' || true)
      if [ -z "\$DISCOVERED" ]; then
        DISCOVERED=\$(smbclient -L "smb://\$SMB_HOST" -U "guest%" -g 2>/dev/null | awk -F'|' '\$1=="Disk" && \$2!~/\$$/ {print \$2; exit}' || true)
      fi

      if [ -n "\$DISCOVERED" ]; then
        SMB_SHARE="/\$DISCOVERED"
        TARGET="//\$SMB_HOST\$SMB_SHARE"
        echo "[+] Found share: \$TARGET"
      else
        echo "[!] Note: Could not auto-list anonymous shares on \$SMB_HOST."
        echo "    Trying common defaults: 'share', 'data', 'public', or PocketCloud label..."
        for try_share in "share" "data" "public" "storage"; do
          if smbclient "//\$SMB_HOST/\$try_share" -N -c "exit" &>/dev/null || smbclient "//\$SMB_HOST/\$try_share" -U "guest%" -c "exit" &>/dev/null; then
            SMB_SHARE="/\$try_share"
            TARGET="//\$SMB_HOST\$SMB_SHARE"
            echo "[+] Verified accessible share: \$TARGET"
            break
          fi
        done
      fi
    fi

    if [ -n "\$SMB_SHARE" ] && [ "\$SMB_SHARE" != "/" ]; then
      MOUNT_OPTS="rw,uid=\$NEON_UID,gid=\$NEON_GID,file_mode=0775,dir_mode=0775,iocharset=utf8,_netdev,nofail"
      if [ -n "\$SMB_PASS" ]; then
        CRED_FILE="/etc/neon-smb.cred"
        cat << CRED_EOF > "\$CRED_FILE"
username=\$SMB_USER
password=\$SMB_PASS
CRED_EOF
        chmod 600 "\$CRED_FILE"
        MOUNT_OPTS="credentials=\$CRED_FILE,\$MOUNT_OPTS"
      else
        MOUNT_OPTS="guest,\$MOUNT_OPTS"
      fi

      echo "[+] Mounting SMB share \$TARGET to \$MOUNT_POINT..."
      if mount -t cifs "\$TARGET" "\$MOUNT_POINT" -o "\$MOUNT_OPTS"; then
        echo "[+] Successfully mounted \$TARGET!"
        FSTAB_LINE="\$TARGET \$MOUNT_POINT cifs \$MOUNT_OPTS 0 0"
        if ! grep -q "\$TARGET" /etc/fstab; then
          echo "\$FSTAB_LINE" >> /etc/fstab
        fi
      else
        echo "[!] Warning: mount -t cifs failed. Check share name or credentials."
        echo "[i] Continuing with local directory so services can start."
      fi
    else
      echo "[!] No SMB share name confirmed. Using local folder for now."
      echo "    You can mount the share anytime with:"
      echo "    sudo mount -t cifs //\$SMB_HOST/<share_name> \$MOUNT_POINT -o guest,uid=\$NEON_UID,gid=\$NEON_GID"
    fi

  elif [ -n "\$TARGET" ] && [ -b "\$TARGET" ]; then
    echo "[i] Block device specified: \$TARGET"
    CURRENT_FS=\$(blkid -s TYPE -o value "\$TARGET" || true)
    if [ -z "\$CURRENT_FS" ]; then
      if [ "\$FORCE_FMT" = "true" ]; then
        echo "[!] Formatting \$TARGET to ext4..."
        mkfs.ext4 -F -L "PocketCloud" "\$TARGET"
      else
        echo "[-] \$TARGET has no filesystem. Re-run with --format to format."
      fi
    fi
    if [ -n "\$(blkid -s TYPE -o value "\$TARGET" || true)" ]; then
      echo "[+] Mounting \$TARGET to \$MOUNT_POINT..."
      mount "\$TARGET" "\$MOUNT_POINT" || true
      DEV_UUID=\$(blkid -s UUID -o value "\$TARGET" || true)
      if [ -n "\$DEV_UUID" ] && ! grep -q "\$DEV_UUID" /etc/fstab; then
        echo "UUID=\$DEV_UUID \$MOUNT_POINT ext4 defaults,noatime,nofail 0 2" >> /etc/fstab
      fi
    fi

  else
    echo "[i] Using local storage directory at \$STORAGE_DIR"
  fi
fi

mkdir -p "\$STORAGE_DIR"
chown -R neon:neon "\$MOUNT_POINT" "\$STORAGE_DIR" 2>/dev/null || true
chmod 775 "\$MOUNT_POINT" "\$STORAGE_DIR" 2>/dev/null || true
echo "[+] NAS storage directory verified: \$STORAGE_DIR"
REMOTE_EOF
)

run_remote_script "$STEP3_SCRIPT"

# 4. Compile Standalone Go Binaries Locally
echo "[4/7] Compiling static binaries for linux/$GOARCH..."
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -ldflags="-s -w" -o "bin/api-server-linux-$GOARCH" ./cmd/api-server
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -ldflags="-s -w" -o "bin/neon-ctl-linux-$GOARCH" ./cmd/neon-ctl
echo "[+] Built: bin/api-server-linux-$GOARCH and bin/neon-ctl-linux-$GOARCH"

# 5. Prepare Remote Installation Directory and Transfer Files
echo "[5/7] Deploying binaries to $REMOTE_HOST:$REMOTE_INSTALL_DIR..."
run_ssh_interactive "sudo mkdir -p $REMOTE_INSTALL_DIR /var/lib/neonservices $NAS_MOUNT_POINT/storage && \
                     sudo chown -R $SSH_USER:$SSH_USER $REMOTE_INSTALL_DIR && \
                     sudo chown -R neon:neon $NAS_MOUNT_POINT"

run_scp "bin/api-server-linux-$GOARCH" "$SSH_USER@$REMOTE_HOST:$REMOTE_INSTALL_DIR/api-server.new"
run_scp "bin/neon-ctl-linux-$GOARCH" "$SSH_USER@$REMOTE_HOST:$REMOTE_INSTALL_DIR/neon-ctl.new"

run_ssh_interactive "chmod +x $REMOTE_INSTALL_DIR/api-server.new $REMOTE_INSTALL_DIR/neon-ctl.new && \
         mv $REMOTE_INSTALL_DIR/api-server.new $REMOTE_INSTALL_DIR/api-server && \
         mv $REMOTE_INSTALL_DIR/neon-ctl.new $REMOTE_INSTALL_DIR/neon-ctl && \
         sudo ln -sf $REMOTE_INSTALL_DIR/neon-ctl /usr/local/bin/neon-ctl || true"

# 6. Setup Configuration and Secrets on Remote
echo "[6/7] Checking remote configuration and secrets..."
STEP6_SCRIPT=$(cat << REMOTE_CONF_EOF
set -euo pipefail
DIR="$REMOTE_INSTALL_DIR"
MOUNT="$NAS_MOUNT_POINT/storage"

mkdir -p "\$MOUNT" "/var/lib/neonservices"

if [ ! -f "\$DIR/config.yaml" ]; then
  echo "[+] Generating initial production config.yaml..."
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
chown -R neon:neon "$REMOTE_INSTALL_DIR" "/var/lib/neonservices" "$NAS_MOUNT_POINT"
chmod 600 "\$DIR/config.yaml"
chmod 750 "$REMOTE_INSTALL_DIR" "/var/lib/neonservices"
REMOTE_CONF_EOF
)

run_remote_script "$STEP6_SCRIPT"

# 7. Install Systemd Service and Restart
echo "[7/7] Installing systemd unit and starting NeonServices..."
run_scp "deploy/systemd/neonservices.service" "$SSH_USER@$REMOTE_HOST:/tmp/neonservices.service"

run_ssh_interactive "sudo cp /tmp/neonservices.service /etc/systemd/system/neonservices.service && \
         sudo systemctl daemon-reload && \
         sudo systemctl enable neonservices && \
         sudo systemctl restart neonservices"

# Health Check Verification
echo ""
echo "[*] Waiting for service to report active status..."
sleep 2

STATUS_OUTPUT=$(run_ssh "systemctl is-active neonservices || true" | tr -d '\r\n')
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
echo " 🎉 Deployment and StationPC PocketCloud NAS Setup Complete!"
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
