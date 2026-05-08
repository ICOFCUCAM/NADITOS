package main

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/audit/internal/anpralerts"
	"github.com/icofcucam/naditos/services/audit/internal/api"
	"github.com/icofcucam/naditos/services/audit/internal/rollup"
)

func main() {
	cfg := config.MustLoad("audit", 8007)
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

	// Officer anomaly rollup: aggregates fines into officer_daily_stats
	// and computes a within-officer z-score every hour. Same pool as
	// the audit log writer because it has the BYPASSRLS role analytics
	// queries need to read across tenants.
	job := rollup.New(pool, log)
	go job.Run(ctx)

	// ANPR alert consumer: each anpr.alert event (stolen / seized /
	// wanted vehicle scanned) lands in audit_alerts so dispatch /
	// admin sees it on the same /audit page as the rollup anomalies.
	go anpralerts.Run(ctx, pool, log)

	h := api.New(cfg, log, pool, issuer, job)
	if err := server.Run(ctx, log, "audit", cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
}
