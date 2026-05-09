// Package config loads typed configuration from environment.
package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Service struct {
	Name           string
	Port           int
	DatabaseURL    string
	RedisURL       string
	JWTSecret      string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	AuditURL       string
	LogLevel       string
	DefaultTenant  string
	DefaultLocale  string
	DefaultCountry string
}

// MustLoad returns the standard service config. JWT_SECRET is always
// required — every service signs or verifies tokens. DATABASE_URL is
// optional here because some services (the gateway) don't talk to
// Postgres; services that do must call MustLoadWithDB or check
// cfg.DatabaseURL themselves.
func MustLoad(name string, defaultPort int) Service {
	c := Service{
		Name:           name,
		Port:           getInt("SERVICE_PORT", defaultPort),
		DatabaseURL:    getStr("DATABASE_URL", ""),
		RedisURL:       getStr("REDIS_URL", ""),
		JWTSecret:      must("JWT_SECRET"),
		AccessTTL:      getDur("JWT_ACCESS_TTL", 15*time.Minute),
		RefreshTTL:     getDur("JWT_REFRESH_TTL", 30*24*time.Hour),
		AuditURL:       getStr("AUDIT_URL", ""),
		LogLevel:       getStr("LOG_LEVEL", "info"),
		DefaultTenant:  getStr("NEXT_PUBLIC_DEFAULT_TENANT", "demo"),
		DefaultLocale:  getStr("DEFAULT_LOCALE", "en"),
		DefaultCountry: getStr("DEFAULT_COUNTRY", "XX"),
	}
	return c
}

// MustLoadWithDB is MustLoad plus a strict DATABASE_URL check. Use this
// from main() of services that connect to Postgres. Failing fast at
// startup with a named env var beats panicking on the first request.
func MustLoadWithDB(name string, defaultPort int) Service {
	c := MustLoad(name, defaultPort)
	if c.DatabaseURL == "" {
		panic(errors.New("missing env var: DATABASE_URL"))
	}
	return c
}

func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic(errors.New("missing env var: " + k))
	}
	return v
}
func getStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func getInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
func getDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
