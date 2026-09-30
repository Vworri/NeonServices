package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/neonphnx/NeonServices/internal/auth"
	"github.com/neonphnx/NeonServices/internal/config"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/server"
	"github.com/neonphnx/NeonServices/internal/storage"
)

var (
	Version   = "1.0.0"
	BuildTime = "dev"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to YAML configuration file")
	initAdmin := flag.Bool("init-admin", false, "Prompt or create default admin account if none exists")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("NeonServices API Server v%s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	log.Printf("[NeonServices] Starting server (version: %s)...", Version)

	// Load Configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load configuration: %v", err)
	}

	// Initialize Database
	log.Printf("[NeonServices] Connecting to SQLite database at: %s", cfg.Database.Path)
	db, err := database.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize Storage Manager (NAS / StationPC PocketCloud)
	log.Printf("[NeonServices] Initializing NAS storage manager at: %s", cfg.Storage.BaseMountPath)
	sm := storage.NewManager(cfg.Storage.BaseMountPath, cfg.Storage.MaxUploadSizeMB)
	if err := sm.EnsureBaseDir(); err != nil {
		log.Printf("[WARN] Base storage directory could not be created: %v (mount may not be attached yet)", err)
	}

	nasStatus, err := sm.GetNASStatus()
	if err != nil || !nasStatus.IsAvailable {
		log.Printf("[WARN] NAS mount status: %s (Path: %s)", nasStatus.StatusMessage, nasStatus.MountPath)
	} else {
		log.Printf("[INFO] NAS Storage online: %.2f GB free of %.2f GB (%.1f%% used)",
			float64(nasStatus.FreeBytes)/(1024*1024*1024),
			float64(nasStatus.TotalBytes)/(1024*1024*1024),
			nasStatus.PercentUsed,
		)
	}

	// Initial Admin creation if requested or if no users exist and in dev mode
	userCount, _ := db.CountUsers()
	if userCount == 0 && *initAdmin {
		adminPass := "admin12345"
		hash, _ := auth.HashPassword(adminPass)
		adminUser := &database.User{
			Username:   "admin",
			Email:      "admin@neon.local",
			PasswordHash: hash,
			Role:       database.RoleAdmin,
			QuotaBytes: cfg.Storage.DefaultQuotaBytes,
		}
		if err := db.CreateUser(adminUser); err == nil {
			log.Printf("[NOTICE] Created initial admin account (username: admin, password: %s)", adminPass)
			log.Println("[NOTICE] Please change this password immediately via the management app!")
		}
	}

	// Start HTTP Server
	srv := server.New(cfg, db, sm)

	// Graceful shutdown listener
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("[FATAL] Server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("[NeonServices] Shutdown signal received, terminating gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[ERROR] Graceful shutdown failed: %v", err)
	}

	log.Println("[NeonServices] Server stopped cleanly.")
}
