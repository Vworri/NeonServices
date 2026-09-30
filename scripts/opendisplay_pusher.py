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
        missing.append('requests')
    try:
        from PIL import Image
    except ImportError:
        missing.append('pillow')
    try:
        from opendisplay import OpenDisplayDevice
    except ImportError:
        missing.append('py-opendisplay')
    
    if missing:
        print(f'[!] Missing required Python packages: {missing}')
        print('[*] Installing required packages via pip...')
        import subprocess
        cmd = [sys.executable, '-m', 'pip', 'install', '--user'] + missing
        res = subprocess.run(cmd)
        if res.returncode != 0:
            print('[!] Failed to auto-install dependencies. Please run:')
            print(f'    pip install --user {" ".join(missing)}')
            sys.exit(1)

check_dependencies()

import requests
from PIL import Image
from opendisplay import OpenDisplayDevice, Rotation

async def push_image(mac: str, image_url: str = None, image_file: str = None, rotate: int = 0) -> bool:
    img = None
    if image_file and os.path.exists(image_file):
        print(f'[*] Loading image from local file: {image_file}')
        try:
            img = Image.open(image_file)
        except Exception as e:
            print(f'[!] Failed to open image file: {e}')
            return False
    elif image_url:
        print(f'[*] Fetching live dashboard from {image_url}...')
        try:
            resp = requests.get(image_url, timeout=15)
            resp.raise_for_status()
            img = Image.open(BytesIO(resp.content))
        except Exception as e:
            print(f'[!] Failed to fetch dashboard from URL: {e}')
            return False
    else:
        print('[!] No image source specified (--url or --image)')
        return False

    print(f'[+] Loaded dashboard image: {img.width}x{img.height}, format={img.format or "PNG"}')

    rot_map = {
        0: Rotation.ROTATE_0,
        90: Rotation.ROTATE_90,
        180: Rotation.ROTATE_180,
        270: Rotation.ROTATE_270,
    }
    rot = rot_map.get(rotate, Rotation.ROTATE_0)

    print(f'[*] Connecting to OpenDisplay device {mac} over BLE...')
    try:
        async with OpenDisplayDevice(mac_address=mac) as dev:
            print('[+] Connected to reTerminal! Uploading frame to e-paper screen...')
            await dev.upload_image(img, rotate=rot)
            print('🎉 Screen refresh complete! Frame displayed on reTerminal E1001.')
            return True
    except Exception as e:
        print(f'[!] OpenDisplay BLE transfer failed: {e}')
        return False

def main():
    parser = argparse.ArgumentParser(description='OpenDisplay Dashboard Pusher for Seeed reTerminal E1001')
    parser.add_argument('--mac', default='AC:27:6E:A6:AA:F5', help='Bluetooth MAC address of the reTerminal')
    parser.add_argument('--url', default='http://192.168.3.54:8080/screen', help='URL of the live dashboard image')
    parser.add_argument('--image', default=None, help='Path to local image file to push')
    parser.add_argument('--rotate', type=int, default=0, choices=[0, 90, 180, 270], help='Rotation angle')
    parser.add_argument('--interval', type=int, default=60, help='Polling/push interval in seconds (default: 60)')
    parser.add_argument('--once', action='store_true', help='Push once and exit immediately')

    args = parser.parse_args()

    if args.once:
        success = asyncio.run(push_image(args.mac, args.url, args.image, args.rotate))
        sys.exit(0 if success else 1)

    print(f'🚀 Starting OpenDisplay Pusher Daemon for {args.mac}')
    print(f'   Target URL: {args.url}')
    print(f'   Refresh interval: {args.interval}s')
    while True:
        try:
            asyncio.run(push_image(args.mac, args.url, args.image, args.rotate))
        except Exception as e:
            print(f'[!] Unexpected error during push loop: {e}')
        time.sleep(args.interval)

if __name__ == '__main__':
    main()
