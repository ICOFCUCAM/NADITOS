package main

import (
	"context"
	"log/slog"

	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/auth/internal/api"
)

func main() {
	cfg := config.MustLoadWithDB("auth", 8001)
	log := logger.New(cfg.LogLevel)
	log.Info("auth: boot",
		slog.Int("port", cfg.Port),
		slog.String("default_tenant", cfg.DefaultTenant),
	)

	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("auth: db open failed", slog.String("err", err.Error()))
		panic(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Error("auth: db ping failed", slog.String("err", err.Error()))
		panic(err)
	}
	log.Info("auth: db ping ok")

	h := api.New(cfg, log, pool)
	if err := server.Run(ctx, log, "auth", cfg.Port, h); err != nil {
		log.Error("auth: server exited", slog.String("err", err.Error()))
	}
}
