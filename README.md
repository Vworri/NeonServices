# NeonServices 🚀

> Multi-User REST API Platform with StationPC PocketCloud NAS Integration, SSH Remote Deployment, and Fyne Desktop Secrets Manager.

Designed for running production Go and Python services on an **Ubuntu machine** attached to a **STATIONPC PocketCloud NAS** (or any local/NVMe/network storage), managed securely over SSH from your workstation.

---

## 🏛️ System Architecture

```mermaid
graph TD
    subgraph Desktop / Workstation
        Fyne["Fyne Management App (cmd/fyne-manager)"]
        CLI["Management CLI (cmd/neon-ctl)"]
        Deploy["Deployment Script (deploy/deploy.sh)"]
    end

    subgraph Ubuntu Server
        SSH["SSH / SFTP Daemon (:22)"]
        Systemd["systemd (neonservices.service)"]
        API["NeonServices REST API (:8080)"]
        DB[(SQLite WAL Database)]
        PythonWorker["Python Companion Worker"]

        subgraph StationPC PocketCloud NAS
            NASMount["/mnt/pocketcloud/storage"]
            UserA["/users/alice/..."]
            UserB["/users/bob/..."]
        end
    end

    Fyne -->|SSH/SFTP (Push Secrets & Config)| SSH
    Fyne -->|systemctl restart & logs| SSH
    CLI -->|SSH / SFTP Ops| SSH
    Deploy -->|SCP Binary & Systemd Reload| SSH

    Systemd -->|ExecStart & Supervise| API
    API -->|Persist Users & Metadata| DB
    API -->|Multi-Tenant File Sandboxing| NASMount
    PythonWorker -->|REST API / Token Auth| API
```

---

## ✨ Features

- **Multi-Tenant User Authentication**: Secure registration, login, role-based access control (Admin / User), and bcrypt password hashing with JWT Bearer tokens.
- **StationPC PocketCloud NAS Storage Engine**:
  - Live capacity monitoring (total, free, used bytes, percentage).
  - Isolated per-user storage sandboxes (`/mnt/pocketcloud/storage/users/{username}`).
  - Strict path-traversal prevention.
  - Streaming file upload and download with HTTP `Range` support (for media playback & resumed downloads).
  - User quota enforcement.
- **Fyne Desktop GUI Control Center (`cmd/fyne-manager`)**:
  - Direct SSH / SFTP connection with key or password authentication.
  - Interactive Secrets & Config manager with 256-bit cryptographic JWT key generator.
  - 1-click remote deployment of `config.yaml` with secure `0600` permissions.
  - Ubuntu `systemd` service status inspection, restart, and live `journalctl` log viewer.
  - Live StationPC PocketCloud NAS storage health and disk space viewer.
- **Headless Companion CLI (`cmd/neon-ctl`)**:
  - Manage services, push/pull configurations, check logs, and inspect NAS status from any terminal without GUI dependencies.
- **Automated SSH Deployment**:
  - Zero-dependency static Linux Go binary compilation (`CGO_ENABLED=0` for `amd64` and `arm64`).
  - Automated deployment script (`deploy/deploy.sh`) and host setup script (`deploy/setup-host.sh`).
- **Python Service Integration**:
  - Python worker template with token authentication and REST API client (`python/nas_worker.py`).

---

## 📁 Repository Layout

```
NeonServices/
├── cmd/
│   ├── api-server/         # Main backend service binary
│   ├── fyne-manager/       # Fyne desktop GUI Control Center
│   └── neon-ctl/           # Headless CLI management tool
├── internal/
│   ├── auth/               # Password hashing, JWT claims & HTTP middleware
│   ├── config/             # YAML and environment variable configuration
│   ├── database/           # Pure-Go SQLite driver & schema migrations
│   ├── handlers/           # REST API HTTP handlers (Auth, Storage, Admin)
│   ├── server/             # HTTP router, CORS, request logging & graceful shutdown
│   ├── storage/            # StationPC PocketCloud NAS manager & sandboxing
│   └── sshutil/            # SSH & SFTP automation engine
├── deploy/
│   ├── deploy.sh           # 1-step build & deploy script over SSH
│   ├── setup-host.sh       # Ubuntu machine bootstrap (users, permissions, systemd)
│   └── systemd/            # Hardened systemd service definition
├── python/                 # Python companion worker template
│   ├── nas_worker.py
│   └── requirements.txt
├── config.example.yaml     # Configuration template
├── Makefile                # Build, test, cross-compile, and deploy commands
└── go.mod / go.sum
```

---

## 🔌 StationPC PocketCloud NAS Integration

The **STATIONPC PocketCloud** NAS attaches to your Ubuntu machine via USB-C / NVMe / High-Speed interface or local network:

1. **Mounting the NAS on Ubuntu**:
   Format or mount your partition to `/mnt/pocketcloud`:
   ```bash
   sudo mkdir -p /mnt/pocketcloud/storage
   sudo mount /dev/sdX1 /mnt/pocketcloud
   ```
   Add to `/etc/fstab` for auto-mounting on boot:
   ```
   /dev/sdX1  /mnt/pocketcloud  ext4  defaults,noatime  0  2
   ```

2. **Storage Structure**:
   ```
   /mnt/pocketcloud/storage/
   └── users/
       ├── alice/
       │   ├── documents/
       │   └── photos/
       └── bob/
           └── backups/
   ```

---

## 🚀 Quickstart (Local Development)

### 1. Build and Run API Server

```bash
# Clone and enter directory
cd /home/neonphnx/Projects/NeonServices

# Copy configuration
cp config.example.yaml config.yaml

# Build and run locally with auto-created test admin
make run
```

### 2. Run Tests

```bash
make test
```

---

## 🖥️ Fyne Desktop Management App

To run the desktop GUI Control Center:

```bash
# Install GUI build dependencies if needed (Debian/Ubuntu):
# sudo apt-get install -y libgl1-mesa-dev xorg-dev

# Run Fyne app
go run ./cmd/fyne-manager
```

### Or use the Headless CLI (`neon-ctl`)

```bash
# Generate a new 256-bit secret key
./bin/neon-ctl gen-secret

# Test SSH connectivity to Ubuntu machine
./bin/neon-ctl ssh-check

# Push local config.yaml to Ubuntu machine
./bin/neon-ctl push-config

# Check remote service status and live logs
./bin/neon-ctl status
./bin/neon-ctl logs

# Check StationPC PocketCloud NAS mount status
./bin/neon-ctl nas-status
```

---

## 📡 REST API Reference

All protected endpoints require the header `Authorization: Bearer <token>`.

### Authentication
| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/auth/register` | Register new user (First user automatically gets Admin role) |
| `POST` | `/api/v1/auth/login` | Authenticate with username & password; returns JWT token |
| `GET` | `/api/v1/auth/me` | Fetch authenticated user profile and live NAS storage usage |

### Storage & StationPC PocketCloud NAS
| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/v1/storage/status` | StationPC PocketCloud NAS mount status and capacity |
| `GET` | `/api/v1/storage/files?path=` | List files and folders in user storage sandbox |
| `POST` | `/api/v1/storage/upload` | Multipart file upload (field: `file`, optional `path`, `filename`) |
| `GET` | `/api/v1/storage/download?path=` | Download or stream file (supports HTTP `Range` headers) |
| `POST` | `/api/v1/storage/mkdir` | Create a folder in user storage (`{"path": "my-folder"}`) |
| `DELETE` | `/api/v1/storage/delete?path=` | Delete a file or empty folder |

### Admin Endpoints (Admin Role Required)
| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/v1/admin/users` | List all registered users, storage usage, and quotas |
| `GET` | `/api/v1/admin/system` | System uptime, memory allocation, and NAS health |

---

## 🚢 Deploying to Ubuntu via SSH

### Step 1: Initial Ubuntu Host Bootstrap
Copy and run `deploy/setup-host.sh` on the Ubuntu target machine once:
```bash
scp -i ~/.ssh/id_ed25519 deploy/setup-host.sh ubuntu@<UBUNTU_IP>:~/
ssh -i ~/.ssh/id_ed25519 ubuntu@<UBUNTU_IP> "sudo bash ~/setup-host.sh"
```

### Step 2: Automated Deployment from Workstation
Edit `ssh` block in `config.yaml` or set environment variables:
```bash
export NEON_DEPLOY_HOST="192.168.1.100"
export NEON_DEPLOY_USER="ubuntu"
export NEON_DEPLOY_KEY="$HOME/.ssh/id_ed25519"

make deploy
```
This automatically cross-compiles the static Linux binary (`bin/api-server-linux-amd64`), securely copies it via SCP, updates the systemd service, and restarts the service!

---

## 🐍 Python Companion Worker

Run the companion Python service on the Ubuntu server or another machine:
```bash
cd python
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

export NEON_API_URL="http://localhost:8080"
export NEON_WORKER_USER="admin"
export NEON_WORKER_PASS="your-password"
python nas_worker.py
```

---

## 📦 Uploading to Git

To push this repository to GitHub or GitLab:

```bash
# Add files and make initial commit
git add .
git commit -m "feat: initial NeonServices multi-user platform with NAS integration"

# Link your remote repository and push
git remote add origin git@github.com:<your-user>/NeonServices.git
git push -u origin main
```
