package main

import (
	"context"
	"os"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/registry/internal/api"
)

func main() {
	cfg := config.MustLoadWithDB("registry", 8002)
	log := logger.New(cfg.LogLevel)

	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db open failed", "err", err)
		panic(err)
	}
	defer pool.Close()

	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	auditCl := audit.New(cfg.AuditURL, "registry")

	// Outbox-relay pattern: producers write to event_outbox inside their
	// tx; the relay drains it to the publisher (NATS or InProc).
	bus := events.OpenPublisher(os.Getenv("NATS_URL"), log)
	relay := events.NewRelay(pool, log, bus)
	go relay.Run(ctx)

	h := api.New(cfg, log, pool, issuer, auditCl, bus)
	if err := server.Run(ctx, log, "registry", cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
