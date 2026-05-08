package main

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/contracts/payments"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/fines/internal/api"
)

func main() {
	cfg := config.MustLoad("fines", 8006)
	log := logger.New(cfg.LogLevel)

	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db open failed", "err", err)
		panic(err)
	}
	defer pool.Close()

	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	auditCl := audit.New(cfg.AuditURL, "fines")

	// Wire Phase-1 default adapters; Phase-2 swaps these to real providers.
	pay := payments.NewDevStub()
	bus := events.NewInProc(log)

	h := api.New(cfg, log, pool, issuer, auditCl, pay, bus)
	if err := server.Run(ctx, log, cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
