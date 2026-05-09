package main

import (
	"context"
	"os"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts/payments"
	"github.com/icofcucam/naditos/packages/go-common/contracts/storage"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/fines/internal/api"
	"github.com/icofcucam/naditos/services/fines/internal/escalation"
	"github.com/icofcucam/naditos/services/fines/internal/reaper"
)

func main() {
	cfg := config.MustLoadWithDB("fines", 8006)
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

	// Phase-1 default adapters; Phase-2 swaps these to real providers.
	pay := payments.NewDevStub()
	hm := connectors.NewHealthMonitor(pool)

	// OpenPublisher picks NATS when NATS_URL is set, otherwise an
	// in-process bus. Producers write to the outbox inside their tx;
	// the relay drains the outbox into this publisher.
	bus := events.OpenPublisher(os.Getenv("NATS_URL"), log)
	relay := events.NewRelay(pool, log, bus)
	go relay.Run(ctx)

	// Background escalation: walks unpaid fines through the per-tenant
	// regulation_escalation stages (warning → penalty → flag → seize →
	// court). Sweep every 5 minutes by default.
	go escalation.New(pool, log).Run(ctx)

	// Background reaper: seals fine_evidence past its retention
	// deadline and deletes the underlying object. Phase-2 default
	// bucket is "evidence"; Phase-4 will pull this from config.
	reap := reaper.New(pool, storage.NewDevStub(), "evidence", log)
	go reap.Run(ctx)

	// The runtime DB user is BYPASSRLS, so the same pool serves both
	// regular and admin uses. Tests pass a distinct admin pool.
	h := api.New(cfg, log, pool, pool, issuer, auditCl, pay, hm, bus, reap)
	if err := server.Run(ctx, log, "fines", cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
