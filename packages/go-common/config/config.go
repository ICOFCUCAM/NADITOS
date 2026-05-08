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

func MustLoad(name string, defaultPort int) Service {
	c := Service{
		Name:           name,
		Port:           getInt("SERVICE_PORT", defaultPort),
		DatabaseURL:    must("DATABASE_URL"),
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
