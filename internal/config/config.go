package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all service configurations
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
	SSH      SSHConfig      `yaml:"ssh,omitempty"`
}

type ServerConfig struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	Env             string        `yaml:"env"` // "development" or "production"
}

type AuthConfig struct {
	JWTSecret         string        `yaml:"jwt_secret"`
	TokenTTL          time.Duration `yaml:"token_ttl"`
	AllowRegistration bool          `yaml:"allow_registration"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type StorageConfig struct {
	// BaseMountPath is where the STATIONPC PocketCloud NAS or local disk is mounted
	BaseMountPath       string   `yaml:"base_mount_path"`
	DefaultQuotaBytes   int64    `yaml:"default_quota_bytes"` // e.g. 50GB = 53687091200
	MaxUploadSizeMB     int64    `yaml:"max_upload_size_mb"`  // Max upload per file
	AllowedExtensions   []string `yaml:"allowed_extensions"`  // Empty means all allowed
	CheckMountAvailable bool     `yaml:"check_mount_available"`
}

type SSHConfig struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	User        string `yaml:"user"`
	KeyPath     string `yaml:"key_path"`
	RemoteDir   string `yaml:"remote_dir"`
	ServiceName string `yaml:"service_name"`
}

// DefaultConfig returns default configurations
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
			Env:             "production",
		},
		Auth: AuthConfig{
			JWTSecret:         "replace-with-a-secure-random-secret-key-32-chars-min",
			TokenTTL:          24 * time.Hour,
			AllowRegistration: true,
		},
		Database: DatabaseConfig{
			Path: "/var/lib/neonservices/neonservices.db",
		},
		Storage: StorageConfig{
			BaseMountPath:       "/mnt/pocketcloud/storage",
			DefaultQuotaBytes:   50 * 1024 * 1024 * 1024, // 50 GB default quota
			MaxUploadSizeMB:     2048,                    // 2 GB max file upload
			AllowedExtensions:   []string{},              // all allowed
			CheckMountAvailable: true,
		},
		SSH: SSHConfig{
			Host:        "192.168.1.100",
			Port:        22,
			User:        "ubuntu",
			KeyPath:     filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519"),
			RemoteDir:   "/opt/neonservices",
			ServiceName: "neonservices",
		},
	}
}

// Load loads configuration from a file with environment variable overrides
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
			}
			// If file does not exist, use defaults + env vars
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("failed to parse yaml config: %w", err)
			}
		}
	}

	// Environment variable overrides
	if host := os.Getenv("NEON_SERVER_HOST"); host != "" {
		cfg.Server.Host = host
	}
	if portStr := os.Getenv("NEON_SERVER_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			cfg.Server.Port = p
		}
	}
	if secret := os.Getenv("NEON_AUTH_JWT_SECRET"); secret != "" {
		cfg.Auth.JWTSecret = secret
	}
	if dbPath := os.Getenv("NEON_DATABASE_PATH"); dbPath != "" {
		cfg.Database.Path = dbPath
	}
	if mountPath := os.Getenv("NEON_STORAGE_MOUNT_PATH"); mountPath != "" {
		cfg.Storage.BaseMountPath = mountPath
	}

	return cfg, nil
}

// Save writes the configuration to a YAML file
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create config dir %q: %w", dir, err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file %q: %w", path, err)
	}
	return nil
}
