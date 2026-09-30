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
        Deploy["Deployment Script (deploy/deploy-to-host.sh)"]
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
    Deploy -->|1-Step Mount & Deploy| SSH

    Systemd -->|ExecStart & Supervise| API
    API -->|Persist Users & Metadata| DB
    API -->|Multi-Tenant File Sandboxing| NASMount
    PythonWorker -->|REST API / Token Auth| API
```

---

## ✨ Features

- **Multi-Tenant User Authentication**: Secure registration, login, role-based access control (Admin / User), and bcrypt password hashing with JWT Bearer tokens.
- **reTerminal E1001 OpenDisplay Service**:
  - Purpose-built for the Seeed Studio reTerminal E1001 (7.5" 800×480 e-Paper display).
  - High-visibility typography: large digital clock, local weather (temp, high/low, conditions, humidity, wind), air quality index (AQI & PM2.5), and upcoming calendar events.
  - Intelligent partial refresh engine: detects when only time changes and signals `X-Refresh-Type: partial` (no screen flicker). Triggers periodic full refreshes to clear ghosting.
  - Configurable via the Fyne desktop app, CLI, and REST API per user/device.
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
- **Automated SSH Deployment & NAS Provisioner (`deploy/deploy-to-host.sh`)**:
  - Single command deployment accepting the server IP.
  - Verifies and auto-mounts the StationPC PocketCloud NAS to `/mnt/pocketcloud`.
  - Configures `/etc/fstab` for auto-mounting on reboot using partition UUID with `nofail`.
  - Automatically provisions system user `neon` and directory permissions.
  - Detects CPU architecture (`x86_64` vs `aarch64`) and cross-compiles static Go binaries (`CGO_ENABLED=0`).
  - Installs and restarts the `neonservices.service` systemd daemon.
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
│   ├── deploy-to-host.sh   # 1-command deployment & NAS mounter script
│   ├── deploy.sh           # Deployment wrapper
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

## 🚢 1-Step SSH Deployment & NAS Provisioning

To deploy to your Ubuntu machine and ensure the StationPC PocketCloud NAS is mounted correctly:

```bash
# Basic usage with IP:
./deploy/deploy-to-host.sh 192.168.1.100

# With specific user and SSH key:
./deploy/deploy-to-host.sh ubuntu@192.168.1.100 -i ~/.ssh/id_ed25519

# Specifying block device and custom mount point:
./deploy/deploy-to-host.sh 192.168.1.100 -d /dev/sdb1 -m /mnt/pocketcloud
```

### What `deploy-to-host.sh` Does Automatically:
1. **Checks SSH Connectivity**: Tests connection with target credentials.
2. **Detects CPU Architecture**: Maps `x86_64` -> `amd64` or `aarch64` -> `arm64`.
3. **Verifies & Mounts the NAS**:
   - Checks if `/mnt/pocketcloud` is already mounted via `findmnt`.
   - If unmounted, locates the block device (or uses `-d <device>`).
   - Mounts the partition to `/mnt/pocketcloud`.
   - Adds the disk UUID to `/etc/fstab` (`defaults,noatime,nofail 0 2`) so it persists on boot.
   - Creates `/mnt/pocketcloud/storage` and sets ownership to `neon:neon`.
4. **Compiles Static Go Binaries**: Statically compiles `api-server` and `neon-ctl` for target Linux architecture.
5. **Transfers Binaries & Installs**: Copies binaries to `/opt/neonservices/` on the server and symlinks `neon-ctl` into `/usr/local/bin`.
6. **Configures Secrets**: If no `config.yaml` exists, generates a secure 256-bit JWT secret and writes configuration with `0600` permissions.
7. **Installs Systemd Service**: Deploys `neonservices.service`, reloads systemd, and starts the service.
8. **Runs Health Check**: Verifies `systemctl is-active` and tests `http://127.0.0.1:8080/api/v1/health`.

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

## 🖥️ Seeed Studio reTerminal E1001 OpenDisplay Service

The service generates an 800×480 monochrome e-Paper dashboard optimized for the 7.5" screen on the **Seeed Studio reTerminal E1001**:

```
+--------------------------------------------------------------------------------+
| reTerminal E1001 • OpenDisplay [reterminal-01]            [PARTIAL • 13:50:00] |
+----------------------------------------------------+---------------------------+
|                                                    | WEATHER • New York, NY    |
|   10:45 AM                                         |  72°F   Partly Cloudy     |
|                                                    |         H: 78°  L: 62°    |
|   WEDNESDAY                                        |         Humidity: 55%     |
|   September 30, 2026                               |---------------------------|
|   📍 New York, NY                                  | AIR QUALITY INDEX (AQI)   |
|                                                    |  AQI 32 • GOOD            |
|                                                    |  PM2.5: 7.8 µg/m³         |
+----------------------------------------------------+---------------------------+
| 📅 UPCOMING SCHEDULE & CALENDAR EVENTS                                         |
|  • 14:00 - 15:00   Team Architecture Review  (Zoom Room 1)                     |
|  • 16:30 - 17:30   reTerminal E1001 Deploy   (Lab Office)                      |
+--------------------------------------------------------------------------------+
```

### Display Endpoints
| URL | Description |
|---|---|
| `GET /api/v1/display/{device_id}/image.png` | 800×480 PNG for OpenDisplay WiFi firmware |
| `GET /api/v1/display/{device_id}/image.bmp` | 800×480 BMP format |
| `GET /api/v1/display/{device_id}/image.raw` | 1-bit packed monochrome buffer (48,000 bytes) |
| `GET /api/v1/display/{device_id}/status` | JSON payload with data and refresh interval |
| `POST /api/v1/display/{device_id}/config` | Update device location, timezone & calendar |
| `POST /api/v1/display/{device_id}/events` | Add quick calendar event to the display |

### OpenDisplay HTTP Headers
Every image request returns headers consumed by OpenDisplay firmware:
- `X-Refresh-Type: partial` (fast refresh, no flashing) or `full` (complete e-ink refresh)
- `X-Next-Poll-Seconds: 60` (or configured interval)

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
