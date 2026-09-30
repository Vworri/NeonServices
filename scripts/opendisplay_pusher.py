#!/usr/bin/env python3
"""
OpenDisplay E-Ink Dashboard Pusher for Seeed Studio reTerminal E1001
Fetches the 800x480 live dashboard from NeonServices and pushes it to OpenDisplay over BLE.
"""
import sys
import os
for p in [
    os.path.expanduser("~/.local/lib/python3.14/site-packages"),
    os.path.expanduser("~/.local/lib/python3.13/site-packages"),
    "/var/home/neonphnx/.local/lib/python3.14/site-packages",
]:
    if os.path.exists(p) and p not in sys.path:
        sys.path.insert(0, p)
import time
import argparse
import asyncio
from io import BytesIO

def check_dependencies():
    missing = []
    try:
        import requests
    except ImportError:
        missing.append("requests")
    try:
        from PIL import Image
    except ImportError:
        missing.append("pillow")
    try:
        from opendisplay import OpenDisplayDevice
    except ImportError:
        missing.append("py-opendisplay")
    
    if missing:
        print(f"[!] Missing required Python packages: {missing}")
        print("[*] Installing required packages via pip...")
        import subprocess
        cmd = [sys.executable, "-m", "pip", "install", "--user"] + missing
        res = subprocess.run(cmd)
        if res.returncode != 0:
            print("[!] Failed to auto-install dependencies. Please run:")
            print(f"    pip install --user {" ".join(missing)}")
            sys.exit(1)

check_dependencies()

import requests
from PIL import Image
from bleak import BleakScanner
from opendisplay import OpenDisplayDevice, Rotation

def fetch_image(image_url: str = None, image_file: str = None):
    if image_file and os.path.exists(image_file):
        try:
            return Image.open(image_file)
        except Exception as e:
            print(f"[!] Failed to open image file: {e}")
            return None
    elif image_url:
        try:
            resp = requests.get(image_url, timeout=12)
            resp.raise_for_status()
            return Image.open(BytesIO(resp.content))
        except Exception as e:
            print(f"[!] Failed to fetch dashboard from URL: {e}")
            return None
    return None

async def push_with_device(ble_device, image_url: str = None, image_file: str = None, rotate: int = 0, sleep_seconds: int = 60) -> bool:
    img = fetch_image(image_url, image_file)
    if not img:
        return False

    rot_map = {
        0: Rotation.ROTATE_0,
        90: Rotation.ROTATE_90,
        180: Rotation.ROTATE_180,
        270: Rotation.ROTATE_270,
    }
    rot = rot_map.get(rotate, Rotation.ROTATE_0)

    try:
        async with OpenDisplayDevice(mac_address=ble_device.address, ble_device=ble_device) as dev:
            print(f"[+] Connected to {ble_device.name}! Uploading 800x480 frame...")
            await dev.upload_image(img, rotate=rot)
            print("🎉 Screen refresh complete! Frame displayed on reTerminal E1001.")
            
            # Program the next wake interval
            if sleep_seconds and sleep_seconds >= 60:
                try:
                    await dev.deep_sleep(duration_seconds=sleep_seconds)
                    print(f"[*] Deep sleep programmed: device will wake in {sleep_seconds}s")
                except Exception:
                    pass
            return True
    except Exception as e:
        print(f"[!] BLE transfer failed: {e}")
        return False

async def push_once(mac: str, image_url: str = None, image_file: str = None, rotate: int = 0, sleep_seconds: int = 60) -> bool:
    print(f"[*] Scanning for reTerminal ({mac})...")
    ble_device = await BleakScanner.find_device_by_address(mac, timeout=15.0)
    if not ble_device:
        print(f"[-] Device {mac} not found during 15s scan.")
        print("    (Note: If device is in deep sleep, press the green wake button to wake it!)")
        return False
    return await push_with_device(ble_device, image_url, image_file, rotate, sleep_seconds)

async def run_daemon(mac: str, image_url: str = None, image_file: str = None, rotate: int = 0, interval: int = 60):
    print(f"🚀 OpenDisplay Smart BLE Sync Daemon Active")
    print(f"   Device MAC: {mac}")
    print(f"   Target URL: {image_url}")
    print(f"   Wake Interval: {interval}s")
    print(f"[*] Actively listening for reTerminal wake broadcasts...")

    last_push_time = 0.0

    while True:
        try:
            # Quick 8-second scan window to detect when device wakes
            ble_device = await BleakScanner.find_device_by_address(mac, timeout=8.0)
            if ble_device:
                now = time.time()
                # Cooldown: only push if at least 25s elapsed since last push
                if now - last_push_time >= min(25, interval):
                    print(f"[+] reTerminal {mac} detected awake! Syncing live dashboard...")
                    success = await push_with_device(ble_device, image_url, image_file, rotate, sleep_seconds=interval)
                    if success:
                        last_push_time = time.time()
                        print(f"[*] Sync complete. Waiting for next wake window...")
        except Exception as e:
            # BlueZ / D-Bus transient error recovery
            await asyncio.sleep(2)
        await asyncio.sleep(2)

def main():
    parser = argparse.ArgumentParser(description="OpenDisplay Dashboard Pusher for Seeed reTerminal E1001")
    parser.add_argument("--mac", default="AC:27:6E:A6:AA:F5", help="Bluetooth MAC address of the reTerminal")
    parser.add_argument("--url", default="http://192.168.3.54:8080/screen", help="URL of the live dashboard image")
    parser.add_argument("--image", default=None, help="Path to local image file to push")
    parser.add_argument("--rotate", type=int, default=0, choices=[0, 90, 180, 270], help="Rotation angle")
    parser.add_argument("--interval", type=int, default=60, help="Refresh interval in seconds (default: 60)")
    parser.add_argument("--once", action="store_true", help="Push once and exit")

    args = parser.parse_args()

    if args.once:
        success = asyncio.run(push_once(args.mac, args.url, args.image, args.rotate, args.interval))
        sys.exit(0 if success else 1)

    asyncio.run(run_daemon(args.mac, args.url, args.image, args.rotate, args.interval))

if __name__ == "__main__":
    main()
