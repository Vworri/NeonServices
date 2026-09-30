package ble

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type DiscoveredDevice struct {
	MAC           string `json:"mac"`
	Name          string `json:"name"`
	IsOpenDisplay bool   `json:"is_opendisplay"`
	Connected     bool   `json:"connected"`
}

// ListDevices lists discovered devices from BlueZ
func ListDevices() ([]DiscoveredDevice, error) {
	cmd := exec.Command("bluetoothctl", "devices")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run bluetoothctl devices: %w", err)
	}

	var devices []DiscoveredDevice
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Device ") {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) >= 2 {
			mac := parts[1]
			name := "Unknown"
			if len(parts) >= 3 {
				name = parts[2]
			}
			isOD := strings.HasPrefix(name, "OD") ||
				strings.Contains(strings.ToLower(name), "opendisplay") ||
				strings.Contains(strings.ToLower(name), "reterminal")

			devices = append(devices, DiscoveredDevice{
				MAC:           mac,
				Name:          name,
				IsOpenDisplay: isOD,
			})
		}
	}
	return devices, nil
}

// ScanAndDiscover triggers a quick BLE scan and returns updated devices
func ScanAndDiscover(timeoutSec int) ([]DiscoveredDevice, error) {
	if timeoutSec <= 0 {
		timeoutSec = 3
	}
	// Run scan in background with timeout
	cmd := exec.Command("bluetoothctl", fmt.Sprintf("--timeout=%d", timeoutSec), "scan", "on")
	_ = cmd.Run()

	return ListDevices()
}

// ConnectDevice connects to a Bluetooth device
func ConnectDevice(mac string) error {
	cmd := exec.Command("bluetoothctl", "connect", mac)
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "Connection successful") {
		return fmt.Errorf("connect failed: %s (%w)", string(out), err)
	}
	return nil
}

// ProvisionOpenDisplay provisions the OpenDisplay device with Wi-Fi and target URL
func ProvisionOpenDisplay(mac, ssid, password, targetURL string) (string, error) {
	if err := ConnectDevice(mac); err != nil {
		return "", fmt.Errorf("could not connect to %s: %w", mac, err)
	}

	time.Sleep(500 * time.Millisecond)

	// Format GATT write commands for OpenDisplay characteristic 00002446-0000-1000-8000-00805f9b34fb
	// We send configuration commands via bluetoothctl gatt menu
	script := fmt.Sprintf(`menu gatt
select-attribute 00002446-0000-1000-8000-00805f9b34fb
write "0x26 %x"
back
quit
`, []byte(fmt.Sprintf("%s\x00%s\x00%s", ssid, password, targetURL)))

	cmd := exec.Command("bluetoothctl")
	cmd.Stdin = bytes.NewBufferString(script)
	out, _ := cmd.CombinedOutput()

	return string(out), nil
}
