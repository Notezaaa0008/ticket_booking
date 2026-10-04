// Package config loads settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               string
	DatabaseURL        string
	TestDatabaseURL    string
	RedisURL           string
	JWTSecret          string
	WebhookSecret      string
	CORSOrigin         string
	SeatHoldTTL        time.Duration
	MockGatewayEnabled bool
}

// Load reads the environment and fails with a clear message when a required variable is missing.
func Load() (*Config, error) {
	var missing []string
	required := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	cfg := &Config{
		Port:            getenv("PORT", "8080"),
		DatabaseURL:     required("DATABASE_URL"),
		TestDatabaseURL: os.Getenv("TEST_DATABASE_URL"),
		RedisURL:        required("REDIS_URL"),
		JWTSecret:       required("JWT_SECRET"),
		WebhookSecret:   required("PAYMENT_WEBHOOK_SECRET"),
		CORSOrigin:      required("CORS_ORIGIN"),
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if cfg.CORSOrigin == "*" {
		return nil, fmt.Errorf("CORS_ORIGIN must be a specific origin, not *")
	}

	ttl, err := strconv.Atoi(getenv("SEAT_HOLD_TTL_SECONDS", "600"))
	if err != nil || ttl <= 0 {
		return nil, fmt.Errorf("SEAT_HOLD_TTL_SECONDS must be a positive integer")
	}
	cfg.SeatHoldTTL = time.Duration(ttl) * time.Second
	cfg.MockGatewayEnabled = getenv("MOCK_GATEWAY_ENABLED", "false") == "true"
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
