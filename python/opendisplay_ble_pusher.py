#!/usr/bin/env python3
"""
NeonServices OpenDisplay BLE / WiFi Companion Pusher
Pushes rendered 800x480 e-Paper images to Seeed Studio reTerminal E1001.
Supports partial refreshes and automatic polling.
"""

import os
import sys
import time
import requests

API_URL = os.getenv("NEON_API_URL", "http://127.0.0.1:8080")
DEVICE_ID = os.getenv("NEON_DEVICE_ID", "reterminal-01")

def fetch_display_image():
    url = f"{API_URL}/api/v1/display/{DEVICE_ID}/image.png"
    try:
        resp = requests.get(url, timeout=10)
        if resp.status_code == 200:
            refresh_type = resp.headers.get("X-Refresh-Type", "partial")
            poll_interval = int(resp.headers.get("X-Next-Poll-Seconds", 60))
            return resp.content, refresh_type, poll_interval
        print(f"[-] Failed to fetch image: HTTP {resp.status_code}")
    except Exception as e:
        print(f"[-] Network error fetching display image: {e}")
    return None, "full", 60

def main():
    print(f"==================================================")
    print(f" reTerminal E1001 OpenDisplay Sync Daemon         ")
    print(f" Target Device: {DEVICE_ID}                       ")
    print(f" Source API:    {API_URL}                         ")
    print(f"==================================================")

    while True:
        image_data, refresh_type, next_poll = fetch_display_image()
        if image_data:
            print(f"[{time.strftime('%X')}] Fetched 800x480 canvas ({len(image_data)} bytes) - Refresh Mode: {refresh_type.upper()}")
            # If using OpenDisplay over WiFi/HTTP: The reTerminal device polls the URL directly.
            # If pushing over BLE: Pass image_data to BLE GATT service 0x2446.
        time.sleep(next_poll)

if __name__ == "__main__":
    main()
