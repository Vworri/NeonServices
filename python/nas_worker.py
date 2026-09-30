#!/usr/bin/env python3
"""
NeonServices Python Companion Worker
Example background automation service for the StationPC PocketCloud NAS.
Demonstrates REST API interaction, token authentication, and file processing.
"""

import os
import sys
import time
import requests
import yaml

class NeonClient:
    def __init__(self, base_url, username, password):
        self.base_url = base_url.rstrip("/")
        self.username = username
        self.password = password
        self.token = None

    def login(self):
        resp = requests.post(
            f"{self.base_url}/api/v1/auth/login",
            json={"username": self.username, "password": self.password},
            timeout=10,
        )
        if resp.status_code == 200:
            data = resp.json()
            self.token = data["data"]["token"]
            print(f"[+] Successfully authenticated as {self.username}")
            return True
        print(f"[-] Login failed ({resp.status_code}): {resp.text}")
        return False

    def headers(self):
        return {"Authorization": f"Bearer {self.token}"}

    def get_nas_status(self):
        resp = requests.get(f"{self.base_url}/api/v1/storage/status", headers=self.headers(), timeout=10)
        return resp.json()

    def list_files(self, path=""):
        resp = requests.get(f"{self.base_url}/api/v1/storage/files", params={"path": path}, headers=self.headers(), timeout=10)
        return resp.json()

def main():
    config_path = os.getenv("NEON_CONFIG", "config.yaml")
    base_url = os.getenv("NEON_API_URL", "http://localhost:8080")
    username = os.getenv("NEON_WORKER_USER", "admin")
    password = os.getenv("NEON_WORKER_PASS", "admin12345")

    print(f"[*] Starting NeonServices Python Companion Worker...")
    print(f"[*] Connecting to {base_url}...")

    client = NeonClient(base_url, username, password)
    if not client.login():
        print("[-] Worker cannot start without valid credentials.")
        sys.exit(1)

    # Periodic background loop
    while True:
        try:
            status = client.get_nas_status()
            print(f"[*] [Worker Heartbeat] NAS Status Check:")
            if "data" in status:
                data = status["data"]
                total_gb = data.get("total_bytes", 0) / (1024**3)
                free_gb = data.get("free_bytes", 0) / (1024**3)
                print(f"    Mount: {data.get('mount_path')} | Free: {free_gb:.2f} GB / {total_gb:.2f} GB ({data.get('percent_used', 0):.1f}% used)")
            
            # Additional worker tasks: media indexing, metadata extraction, backup sync, etc.
        except Exception as e:
            print(f"[!] Worker error: {e}")

        time.sleep(60)

if __name__ == "__main__":
    main()
