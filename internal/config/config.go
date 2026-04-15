package config

import (
	"fmt"
	"os"
)

// Config holds all server configuration.
type Config struct {
	Port         string
	JWTSecret    string
	SynapseURL   string
	LastSeenFile string
	AuthMode     string // "synapse" (default) or "dev"
	DevPassword  string // only used when AuthMode == "dev"
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		Port:         envOr("PORT", "8080"),
		SynapseURL:   envOr("SYNAPSE_URL", "http://localhost:8008"),
		LastSeenFile: envOr("LAST_SEEN_FILE", "last_seen.json"),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		AuthMode:     envOr("AUTH_MODE", "synapse"),
		DevPassword:  os.Getenv("DEV_PASSWORD"),
	}

	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}

	if cfg.AuthMode == "dev" && cfg.DevPassword == "" {
		return nil, fmt.Errorf("DEV_PASSWORD is required when AUTH_MODE=dev")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
