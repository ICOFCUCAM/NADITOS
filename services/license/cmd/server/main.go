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
	"github.com/icofcucam/naditos/services/license/internal/api"
	"github.com/icofcucam/naditos/services/license/internal/demerit"
)

func main() {
	cfg := config.MustLoadWithDB("license", 8003)
	log := logger.New(cfg.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db open failed", "err", err)
		panic(err)
	}
	defer pool.Close()

	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	auditCl := audit.New(cfg.AuditURL, "license")

	// Bus + demerit subscriber. The demerit engine reacts to fine.issued
	// events that originate in OTHER processes (the fines service); it
	// learns about them via the consumer below, not via direct calls.
	localBus := events.NewInProc(log)
	demerit.New(pool, log, auditCl, localBus).Wire(localBus)

	// Producer-side relay drains events emitted by THIS service
	// (license.suspended, license.reinstated, license.demerit) into
	// whichever transport is configured. In dev it's a no-op fan-out
	// — the canonical home for cross-process delivery is the consumer
	// loop below.
	bus := events.OpenPublisher(os.Getenv("NATS_URL"), log)
	relay := events.NewRelay(pool, log, bus)
	go relay.Run(ctx)

	// Cross-process delivery via a per-consumer offset. Without this
	// the demerit engine never sees fine.issued events emitted by the
	// fines service in dev (the producer-side relays compete for rows
	// via SKIP LOCKED, so the license replica's relay would just miss
	// rows that fines's relay already claimed).
	go events.NewConsumer(pool, log, "license-demerit",
		func(ctx context.Context, env events.Envelope) error {
			return localBus.Publish(ctx, env)
		},
	).OnlyTypes(events.TypeFineIssued).Run(ctx)

	h := api.New(cfg, log, pool, issuer, auditCl, localBus)
	if err := server.Run(ctx, log, "license", cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
