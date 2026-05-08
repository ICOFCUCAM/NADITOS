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

	// Demerit subscribes to the local InProc bus regardless of whether
	// the canonical bus is NATS — the relay forwards to both. Wiring is
	// the same in dev and prod.
	localBus := events.NewInProc(log)
	demerit.New(pool, log, auditCl, localBus).Wire(localBus)

	// In dev we publish straight to localBus. In prod with NATS, the
	// outbox relay forwards every event to NATS; a separate JetStream
	// consumer in this process re-injects the events into localBus so
	// demerit fires identically.
	bus := events.OpenPublisher(os.Getenv("NATS_URL"), log)
	relay := events.NewRelay(pool, log, fanOut{primary: bus, local: localBus})
	go relay.Run(ctx)

	h := api.New(cfg, log, pool, issuer, auditCl, localBus)
	if err := server.Run(ctx, log, "license", cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}

// fanOut is a Publisher that forwards every event to a primary
// transport (NATS in prod, in-process in dev) AND to a local InProc
// bus. Subscribers in this process — like the demerit engine — only
// know about localBus.
type fanOut struct {
	primary events.Publisher
	local   events.Publisher
}

func (f fanOut) Publish(ctx context.Context, env events.Envelope) error {
	_ = f.local.Publish(ctx, env)
	return f.primary.Publish(ctx, env)
}
func (f fanOut) Close() error {
	_ = f.local.Close()
	return f.primary.Close()
}
