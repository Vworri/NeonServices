#!/usr/bin/env bash
set -e

echo "=================================================="
echo "  NeonServices OpenDisplay BLE Pusher Installer   "
echo "=================================================="

# Ensure python3 and pip are available
if ! command -v python3 &>/dev/null; then
    echo "[!] python3 is required. Please install python3."
    exit 1
fi

echo "[*] Installing Python dependencies (py-opendisplay, pillow, requests)..."
python3 -m pip install --user --upgrade py-opendisplay pillow requests

echo ""
echo "✅ Dependencies installed successfully!"
echo "You can now run:"
echo "  ./scripts/opendisplay_pusher.py --mac AC:27:6E:A6:AA:F5 --once"
echo "=================================================="
