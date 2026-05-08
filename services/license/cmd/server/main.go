package main

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/license/internal/api"
	"github.com/icofcucam/naditos/services/license/internal/demerit"
)

func main() {
	cfg := config.MustLoad("license", 8003)
	log := logger.New(cfg.LogLevel)

	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db open failed", "err", err)
		panic(err)
	}
	defer pool.Close()

	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	auditCl := audit.New(cfg.AuditURL, "license")
	bus := events.NewInProc(log)

	// Wire the demerit engine to listen for fine.issued and update driver
	// standing. Local subscriptions in Phase-1; in production with NATS
	// this becomes a JetStream durable consumer on the same subject.
	demerit.New(pool, log, auditCl, bus).Wire(bus)

	h := api.New(cfg, log, pool, issuer, auditCl, bus)
	if err := server.Run(ctx, log, cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
