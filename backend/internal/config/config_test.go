package config

import (
	"strings"
	"testing"
	"time"
)

var requiredKeys = []string{"DATABASE_URL", "REDIS_URL", "JWT_SECRET", "PAYMENT_WEBHOOK_SECRET", "CORS_ORIGIN"}

func TestLoadReportsMissingVariables(t *testing.T) {
	for _, k := range requiredKeys {
		t.Setenv(k, "")
	}
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when required variables are missing")
	}
	for _, k := range requiredKeys {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error should mention %s, got: %v", k, err)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	for _, k := range requiredKeys {
		t.Setenv(k, "value")
	}
	t.Setenv("PORT", "")
	t.Setenv("SEAT_HOLD_TTL_SECONDS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("default port = %q, want 8080", cfg.Port)
	}
	if cfg.SeatHoldTTL != 600*time.Second {
		t.Errorf("default hold ttl = %v, want 10m", cfg.SeatHoldTTL)
	}
}

func TestLoadRejectsBadTTL(t *testing.T) {
	for _, k := range requiredKeys {
		t.Setenv(k, "value")
	}
	t.Setenv("SEAT_HOLD_TTL_SECONDS", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a non-numeric ttl")
	}
}
