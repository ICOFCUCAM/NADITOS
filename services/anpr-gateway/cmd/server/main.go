// ANPR gateway — Phase-2 implementation.
//
// HTTP layer accepts scan submissions from authenticated officer devices
// and from edge cameras. Each request is enqueued in anpr_jobs; an
// in-process worker drains the queue, normalizes plates, deduplicates,
// matches vehicles, and emits anpr.scan / anpr.matched / anpr.alert
// events. The worker is deployable as a separate process (-mode=worker).
package main

import (
	"context"
	"flag"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts/anpr"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/anpr-gateway/internal/api"
	"github.com/icofcucam/naditos/services/anpr-gateway/internal/pipeline"
)

func main() {
	mode := flag.String("mode", "both", "api|worker|both")
	flag.Parse()

	cfg := config.MustLoad("anpr-gateway", 8008)
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

	if *mode == "worker" || *mode == "both" {
		w := pipeline.New(pool, log, bus)
		go w.Run(ctx)
		log.Info("anpr worker started")
	}
	if *mode == "api" || *mode == "both" {
		// ANPR_PROVIDER=openalpr + OPENALPR_SECRET_KEY → real upstream;
		// otherwise the dev-stub returns no detections so the police PWA
		// stays in manual-entry mode without any extra config.
		recognizer := anpr.NewFromEnv(log)
		log.Info("anpr provider wired",
			"provider", recognizer.Info().Provider)
		// Same fail-streak tracking insurance and inspection use, so
		// the /providers admin page can show a unified state across
		// every external integration.
		health := connectors.NewHealthMonitor(pool)
		h := api.New(cfg, log, pool, issuer, recognizer, health)
		if err := server.Run(ctx, log, "anpr-gateway", cfg.Port, h); err != nil {
			log.Error("server exited", "err", err)
		}
	} else {
		<-ctx.Done()
	}
}
