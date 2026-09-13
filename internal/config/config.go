package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Host        string
	Port        int
	ForwardURL  string
	DBPath      string
	OpenBrowser bool
	LogLevel    string
	Public      bool
	TunnelURL   string
}

func Default() Config {
	cfg := Config{
		Host:     "127.0.0.1",
		Port:     4040,
		DBPath:   defaultDBPath(),
		LogLevel: "info",
	}

	if value := os.Getenv("HOOKSTASH_HOST"); value != "" {
		cfg.Host = value
	}
	if value := os.Getenv("HOOKSTASH_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			cfg.Port = port
		}
	}
	if value := os.Getenv("HOOKSTASH_FORWARD_URL"); value != "" {
		cfg.ForwardURL = value
	}
	if value := os.Getenv("HOOKSTASH_DB_PATH"); value != "" {
		cfg.DBPath = value
	}
	if value := os.Getenv("HOOKSTASH_LOG_LEVEL"); value != "" {
		cfg.LogLevel = value
	}
	if isTruthy(os.Getenv("HOOKSTASH_PUBLIC")) {
		cfg.Public = true
	}
	if value := os.Getenv("HOOKSTASH_TUNNEL_URL"); value != "" {
		cfg.TunnelURL = value
	}

	return cfg
}

func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "hookstash.db"
	}
	return filepath.Join(home, ".hookstash", "hookstash.db")
}
