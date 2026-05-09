// Notifications service — Phase-3.
//
// Two responsibilities:
//
//	1. HTTP API for ad-hoc sends from operator tooling and admin UI.
//	2. A consumer that drains event_outbox and turns interesting events
//	   (fine.issued, fine.paid, license.suspended) into outbound SMS /
//	   email / push messages via the configured provider.
//
// Phase-1 default adapter is the dev-stub Sender that logs the message;
// Phase-2 swaps in Twilio / Vonage / Sendgrid / sovereign equivalents
// behind the same notifications.Sender contract.
package main

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/contracts/notifications"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/notifications/internal/api"
	"github.com/icofcucam/naditos/services/notifications/internal/consumer"
)

func main() {
	cfg := config.MustLoadWithDB("notifications", 8009)
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

	// Phase-1 stub provider; bind real ones per tenant in Phase-2.
	sender := notifications.NewDevStub(log)

	// Background consumer drains event_outbox and sends notifications.
	go consumer.New(pool, log, sender).Run(ctx)

	h := api.New(cfg, log, pool, issuer, sender)
	if err := server.Run(ctx, log, "notifications", cfg.Port, h); err != nil {
		log.Error("server exited", "err", err)
	}
	_ = httpx.DebugErrors
}
