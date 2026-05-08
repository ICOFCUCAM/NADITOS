// Insurance verification service — Phase-2.
//
// Per-tenant provider routing. Three operating modes:
//
//   1. Online verify (POST /v1/insurance/verify) — caller asks "is this
//      vehicle insured today?". The handler routes to the active
//      provider, updates insurance_records cache + provider_health, and
//      returns the answer.
//
//   2. Webhook (POST /v1/insurance/webhooks/{provider}) — providers push
//      policy changes; we update insurance_records and emit events.
//
//   3. Background reconcile job — pulls vehicles whose
//      insurance_expires_at is in the past or NULL and enqueues a
//      retry-queued verify job. Workers drain the queue with backoff.
package main

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts/insurance"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/insurance/internal/api"
	"github.com/icofcucam/naditos/services/insurance/internal/worker"
)

func main() {
	cfg := config.MustLoad("insurance", 8004)
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
	bus := events.NewInProc(log)

	// Provider routing — Phase-1 dev stub; real adapters bind themselves at boot.
	router := connectors.NewRouter[insurance.Verifier]()
	router.SetDefault(insurance.NewDevStub())

	health := connectors.NewHealthMonitor(pool)
	queue := connectors.NewRetryQueue(pool)

	// Background worker drains the verify retry queue.
	go worker.New(pool, log, router, health, queue, bus).Run(ctx)

	h := api.New(cfg, log, pool, issuer, router, health, queue)
	if err := server.Run(ctx, log, cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
