package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/auth/internal/api"
)

// BuildMarker is a hard-coded string bumped on every commit that touches
// the auth service. Both startup and every login attempt print it via
// raw os.Stderr — a flight recorder. If `fly logs -a naditos-auth` does
// not show this exact string at startup, the deployed binary is not
// built from this source tree (stale checkout, build cache, wrong
// branch, or rolling-deploy traffic split).
const BuildMarker = "auth-build-2026-05-09-6813540+marker1"

func main() {
	fmt.Fprintln(os.Stderr, "AUTH_BUILD_MARKER="+BuildMarker)

	cfg := config.MustLoad("auth", 8001)
	log := logger.New(cfg.LogLevel)
	log.Info("auth: boot",
		slog.String("build_marker", BuildMarker),
		slog.Int("port", cfg.Port),
		slog.String("default_tenant", cfg.DefaultTenant),
		slog.Bool("jwt_secret_set", cfg.JWTSecret != ""),
		slog.Int("jwt_secret_len", len(cfg.JWTSecret)),
		slog.Bool("database_url_set", cfg.DatabaseURL != ""),
		slog.Duration("access_ttl", cfg.AccessTTL),
		slog.Duration("refresh_ttl", cfg.RefreshTTL),
	)

	// AUTH_DEBUG_ERRORS=1 is a temporary debug switch: it makes 500
	// responses include the underlying error string in the JSON body
	// instead of "internal error". Useful while tracking down why
	// login fails; turn it off again before going to production.
	if os.Getenv("AUTH_DEBUG_ERRORS") != "" {
		httpx.DebugErrors = true
		log.Warn("auth: AUTH_DEBUG_ERRORS=1 — leaking internal errors in 500 responses")
		fmt.Fprintln(os.Stderr, "AUTH_DEBUG_ERRORS=1 — leaking internal errors in 500 responses")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("auth: db open failed", slog.String("err", err.Error()))
		fmt.Fprintln(os.Stderr, "AUTH_DB_OPEN_FAILED err="+err.Error())
		panic(err)
	}
	defer pool.Close()
	if pool == nil {
		log.Error("auth: db.Open returned nil pool with no error")
		panic("db pool is nil")
	}

	if err := pool.Ping(ctx); err != nil {
		log.Error("auth: db ping failed", slog.String("err", err.Error()))
		fmt.Fprintln(os.Stderr, "AUTH_DB_PING_FAILED err="+err.Error())
		panic(err)
	}
	log.Info("auth: db ping ok")

	h := api.New(cfg, log, pool)
	if h == nil {
		log.Error("auth: api.New returned nil handler")
		panic("api.New returned nil handler")
	}

	log.Info("auth: handler wired, starting server", slog.Int("port", cfg.Port))
	if err := server.Run(ctx, log, "auth", cfg.Port, h); err != nil {
		log.Error("auth: server exited", slog.String("err", err.Error()))
	}
}
