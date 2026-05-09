package config_test

import (
	"testing"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/config"
)

// TestMustLoad_DefaultsAndOverrides: required env vars are read,
// optional ones fall back to defaults, and explicit overrides win.
// MustLoad is the only entry point services use, so this is the
// regression gate against bad defaults shipping.
func TestMustLoad_DefaultsAndOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("JWT_ACCESS_TTL", "5m")

	c := config.MustLoad("svc", 9000)

	if c.Name != "svc" {
		t.Errorf("name: %s", c.Name)
	}
	if c.Port != 9000 {
		t.Errorf("default port not used: %d", c.Port)
	}
	if c.DatabaseURL != "postgres://x" {
		t.Errorf("db: %s", c.DatabaseURL)
	}
	if c.JWTSecret != "s" {
		t.Errorf("jwt: %s", c.JWTSecret)
	}
	if c.LogLevel != "debug" {
		t.Errorf("log_level override missed: %s", c.LogLevel)
	}
	if c.AccessTTL != 5*time.Minute {
		t.Errorf("access ttl override missed: %s", c.AccessTTL)
	}
	// RefreshTTL falls back to default 30d
	if c.RefreshTTL != 30*24*time.Hour {
		t.Errorf("refresh ttl default missed: %s", c.RefreshTTL)
	}
	// DefaultTenant falls back to "demo"
	if c.DefaultTenant != "demo" {
		t.Errorf("default tenant: %s", c.DefaultTenant)
	}
}

// TestMustLoad_PortOverride: SERVICE_PORT env wins over the default.
func TestMustLoad_PortOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "x")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("SERVICE_PORT", "12345")
	c := config.MustLoad("svc", 8000)
	if c.Port != 12345 {
		t.Fatalf("port: %d", c.Port)
	}
}

// TestMustLoad_AllowsMissingDatabaseURL: the gateway service has no DB
// dependency, so MustLoad must accept an empty DATABASE_URL. Services
// that need a DB call MustLoadWithDB instead.
func TestMustLoad_AllowsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "s")
	c := config.MustLoad("svc", 8000)
	if c.DatabaseURL != "" {
		t.Fatalf("DatabaseURL: want empty, got %q", c.DatabaseURL)
	}
}

// TestMustLoadWithDB_PanicsOnMissingDatabaseURL: services that connect
// to Postgres should call MustLoadWithDB, which fails fast at boot
// rather than 500-ing on every request.
func TestMustLoadWithDB_PanicsOnMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "s")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on missing DATABASE_URL")
		}
	}()
	_ = config.MustLoadWithDB("svc", 8000)
}

// TestMustLoad_BadDurationFallsBackToDefault: a malformed
// JWT_ACCESS_TTL silently falls back to the default — the service
// boots with a known-good value rather than crashing.
func TestMustLoad_BadDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "x")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("JWT_ACCESS_TTL", "not-a-duration")
	c := config.MustLoad("svc", 8000)
	if c.AccessTTL != 15*time.Minute {
		t.Fatalf("bad-duration fallback: got %s", c.AccessTTL)
	}
}
