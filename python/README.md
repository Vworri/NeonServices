# NeonServices Python Companion Services

This directory contains Python services and background automation workers that interact with the NeonServices Go backend and StationPC PocketCloud NAS.

## Setup

1. Create a Python virtual environment:
   ```bash
   python3 -m venv .venv
   source .venv/bin/activate
   pip install -r requirements.txt
   ```

2. Run the companion worker:
   ```bash
   export NEON_API_URL="http://localhost:8080"
   export NEON_WORKER_USER="admin"
   export NEON_WORKER_PASS="your-admin-password"
   python nas_worker.py
   ```

## Deploying as a Systemd Service

To run this Python worker continuously in the background on your Ubuntu machine:

```ini
[Unit]
Description=NeonServices Python NAS Worker
After=neonservices.service

[Service]
Type=simple
User=neon
WorkingDirectory=/opt/neonservices/python
ExecStart=/opt/neonservices/python/.venv/bin/python nas_worker.py
Restart=always
RestartSec=10s
Environment=NEON_API_URL=http://127.0.0.1:8080

[Install]
WantedBy=multi-user.target
```
