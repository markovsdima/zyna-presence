package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port               string
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	PresenceTTL        time.Duration
	HMACSecret         string
	RateLimitIP        int
	RateLimitHeartbeat time.Duration
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:               envOr("PORT", "8080"),
		RedisAddr:          envOr("REDIS_ADDR", "localhost:6379"),
		RedisPassword:      os.Getenv("REDIS_PASSWORD"),
		HMACSecret:         os.Getenv("HMAC_SECRET"),
	}

	if cfg.HMACSecret == "" {
		return nil, fmt.Errorf("HMAC_SECRET is required")
	}

	redisDB, err := strconv.Atoi(envOr("REDIS_DB", "0"))
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB: %w", err)
	}
	cfg.RedisDB = redisDB

	ttl, err := time.ParseDuration(envOr("PRESENCE_TTL", "30s"))
	if err != nil {
		return nil, fmt.Errorf("invalid PRESENCE_TTL: %w", err)
	}
	cfg.PresenceTTL = ttl

	rateLimitIP, err := strconv.Atoi(envOr("RATE_LIMIT_IP", "60"))
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_IP: %w", err)
	}
	cfg.RateLimitIP = rateLimitIP

	heartbeatInterval, err := time.ParseDuration(envOr("RATE_LIMIT_HEARTBEAT", "5s"))
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_HEARTBEAT: %w", err)
	}
	cfg.RateLimitHeartbeat = heartbeatInterval

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
