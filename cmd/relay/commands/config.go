package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultPort     = 7474
	DefaultDaemonHost = "http://localhost"
	Version         = "1.0.0"
)

// Config holds the relay client configuration.
type Config struct {
	Port     int    `json:"port"`
	BaseDir  string `json:"base_dir"`
	APIToken string `json:"api_token"`
	Host     string `json:"host"`
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Port:    DefaultPort,
		BaseDir: filepath.Join(home, ".relay"),
		Host:    DefaultDaemonHost,
	}
}

func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".relay", "config.json")
}

func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()
	path := ConfigPath()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(ConfigPath()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), data, 0600)
}

func DaemonURL(cfg *Config) string {
	return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
}

func PIDFile(cfg *Config) string {
	return filepath.Join(cfg.BaseDir, "daemon.pid")
}
