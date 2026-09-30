package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/neonphnx/NeonServices/internal/config"
	"github.com/neonphnx/NeonServices/internal/sshutil"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	configPath := "config.yaml"
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("Warning: Could not load %s, using defaults: %v\n", configPath, err)
		cfg = config.DefaultConfig()
	}

	command := os.Args[1]

	switch command {
	case "gen-secret":
		bytes := make([]byte, 32)
		if _, err := rand.Read(bytes); err != nil {
			fmt.Printf("Error generating random bytes: %v\n", err)
			os.Exit(1)
		}
		secret := hex.EncodeToString(bytes)
		fmt.Printf("Generated 256-bit Secure Secret:\n%s\n", secret)

	case "ssh-check":
		client := getClient(cfg)
		defer client.Close()
		fmt.Printf("Connecting to %s@%s:%d...\n", cfg.SSH.User, cfg.SSH.Host, cfg.SSH.Port)
		if err := client.Connect(); err != nil {
			fmt.Printf("SSH connection failed: %v\n", err)
			os.Exit(1)
		}
		out, err := client.Run("uname -a && uptime")
		if err != nil {
			fmt.Printf("Failed to run remote probe: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("SUCCESS! Remote system info:\n%s\n", out)

	case "push-config":
		client := getClient(cfg)
		defer client.Close()
		if err := client.Connect(); err != nil {
			fmt.Printf("SSH connection failed: %v\n", err)
			os.Exit(1)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("Failed to read local %s: %v\n", configPath, err)
			os.Exit(1)
		}
		remoteFile := fmt.Sprintf("%s/config.yaml", cfg.SSH.RemoteDir)
		fmt.Printf("Pushing %s to %s on %s...\n", configPath, remoteFile, cfg.SSH.Host)
		if err := client.UploadContent(remoteFile, data, 0600); err != nil {
			fmt.Printf("Failed to upload config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Config pushed successfully with 0600 permissions.")

	case "pull-config":
		client := getClient(cfg)
		defer client.Close()
		if err := client.Connect(); err != nil {
			fmt.Printf("SSH connection failed: %v\n", err)
			os.Exit(1)
		}
		remoteFile := fmt.Sprintf("%s/config.yaml", cfg.SSH.RemoteDir)
		fmt.Printf("Fetching %s from %s...\n", remoteFile, cfg.SSH.Host)
		data, err := client.DownloadContent(remoteFile)
		if err != nil {
			fmt.Printf("Failed to download config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n--- REMOTE CONFIG ---\n%s\n", string(data))

	case "status":
		client := getClient(cfg)
		defer client.Close()
		if err := client.Connect(); err != nil {
			fmt.Printf("SSH connection failed: %v\n", err)
			os.Exit(1)
		}
		status, isActive, _ := client.ServiceStatus(cfg.SSH.ServiceName)
		fmt.Printf("Service %q status: %s (Active: %t)\n", cfg.SSH.ServiceName, status, isActive)

	case "restart":
		client := getClient(cfg)
		defer client.Close()
		if err := client.Connect(); err != nil {
			fmt.Printf("SSH connection failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Restarting %s on %s...\n", cfg.SSH.ServiceName, cfg.SSH.Host)
		out, err := client.RestartService(cfg.SSH.ServiceName)
		if err != nil {
			fmt.Printf("Restart error: %v\nOutput: %s\n", err, out)
			os.Exit(1)
		}
		fmt.Println("Service restarted successfully.")

	case "logs":
		client := getClient(cfg)
		defer client.Close()
		if err := client.Connect(); err != nil {
			fmt.Printf("SSH connection failed: %v\n", err)
			os.Exit(1)
		}
		lines := 50
		out, err := client.ServiceLogs(cfg.SSH.ServiceName, lines)
		if err != nil {
			fmt.Printf("Error fetching logs: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(out)

	case "nas-status":
		client := getClient(cfg)
		defer client.Close()
		if err := client.Connect(); err != nil {
			fmt.Printf("SSH connection failed: %v\n", err)
			os.Exit(1)
		}
		mount := cfg.Storage.BaseMountPath
		fmt.Printf("Querying NAS mount at %s...\n", mount)
		out, err := client.Run(fmt.Sprintf("df -h %s && ls -la %s 2>&1", mount, mount))
		if err != nil {
			fmt.Printf("NAS query error: %v\n%s\n", err, out)
		} else {
			fmt.Printf("\n--- STATIONPC POCKETCLOUD NAS STATUS ---\n%s\n", out)
		}

	default:
		printUsage()
		os.Exit(1)
	}
}

func getClient(cfg *config.Config) *sshutil.Client {
	return sshutil.NewClient(sshutil.Options{
		Host:    cfg.SSH.Host,
		Port:    cfg.SSH.Port,
		User:    cfg.SSH.User,
		KeyPath: cfg.SSH.KeyPath,
	})
}

func printUsage() {
	fmt.Print(`NeonServices Management CLI (neon-ctl)

Usage:
  neon-ctl <command>

Commands:
  ssh-check     Test SSH connection to the remote Ubuntu machine
  push-config   Deploy local config.yaml to remote /opt/neonservices/config.yaml via SFTP
  pull-config   Fetch and display remote config.yaml from Ubuntu host
  status        Check remote systemd service status
  restart       Restart remote systemd service
  logs          Show recent remote systemd journal logs
  nas-status    Inspect StationPC PocketCloud NAS mount status and capacity
  gen-secret    Generate a high-entropy 256-bit hexadecimal secret key
`)
}
