package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
)

// Config holds all configuration for vless-audit.
type Config struct {
	// Listen is the HTTP listen address (default ":8080").
	Listen string `json:"listen"`

	// DBPath is the SQLite database file path.
	DBPath string `json:"db_path"`

	// XrayAPI is the gRPC address of Xray's API service (default "127.0.0.1:10085").
	XrayAPI string `json:"xray_api"`

	// AccessLog is the path to Xray's JSON access log.
	AccessLog string `json:"access_log"`

	// PollIntervalSec is the interval in seconds to poll Xray stats (default 10).
	PollIntervalSec int `json:"poll_interval_sec"`

	// RetentionDays is how many days of traffic data to keep (default 365).
	RetentionDays int `json:"retention_days"`

	// AuthToken is the bearer token for dashboard authentication.
	// Auto-generated on first run if empty.
	AuthToken string `json:"auth_token"`

	// XrayConfigPath is the path to Xray's config.json for user sync.
	XrayConfigPath string `json:"xray_config_path"`

	// XrayBinPath is the path to the Xray binary for restart on user change.
	XrayBinPath string `json:"xray_bin_path"`

	// RegisterSecret is a passphrase required for self-service registration.
	// If empty, registration is disabled.
	RegisterSecret string `json:"register_secret"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Listen:          ":8080",
		DBPath:          "./vless-audit.db",
		XrayAPI:         "127.0.0.1:10085",
		AccessLog:       "/var/log/xray/access.log",
		PollIntervalSec: 10,
		RetentionDays:   365,
	}
}

// Load reads config from a JSON file. If the file doesn't exist,
// it returns defaults and optionally writes them to the path.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First run: generate token, write config, return.
			cfg.AuthToken = generateToken()
			b, _ := json.MarshalIndent(cfg, "", "  ")
			_ = os.WriteFile(path, b, 0o600)
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Fill zero values with defaults.
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./vless-audit.db"
	}
	if cfg.AccessLog == "" {
		cfg.AccessLog = "/var/log/xray/access.log"
	}
	if cfg.PollIntervalSec < 0 {
		cfg.PollIntervalSec = 10
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 365
	}
	if cfg.AuthToken == "" {
		cfg.AuthToken = generateToken()
		// Save the generated token back to config file.
		b, _ := json.MarshalIndent(cfg, "", "  ")
		_ = os.WriteFile(path, b, 0o600)
	}

	return cfg, nil
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
