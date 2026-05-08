package main

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/audit/internal/api"
)

func main() {
	cfg := config.MustLoad("audit", 8007)
	log := logger.New(cfg.LogLevel)

	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db open failed", "err", err)
		panic(err)
	}
	defer pool.Close()

	h := api.New(cfg, log, pool)
	if err := server.Run(ctx, log, "audit", cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
