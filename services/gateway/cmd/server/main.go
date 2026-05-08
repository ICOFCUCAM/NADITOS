// NADITOS API gateway.
//
// Responsibilities:
//   - terminate client TLS (when behind a load balancer it's HTTP)
//   - inject a request-id and a per-request structured log
//   - verify the JWT once and forward it as-is to upstream services
//     (defense-in-depth: services re-verify, but the gateway does the
//     hot-path early reject)
//   - per-tenant + per-IP token-bucket rate limiting
//   - reverse-proxy to one of the upstream services based on URL prefix
//
// Routing table:
//
//   /v1/auth/*          -> AUTH_URL
//   /v1/admin/users     -> AUTH_URL
//   /v1/vehicles*       -> REGISTRY_URL
//   /v1/fines*          -> FINES_URL
//   /v1/audit/*         -> AUDIT_URL  (admin only)
//   /v1/licenses/*      -> LICENSE_URL
//   /v1/anpr/*          -> ANPR_URL
//   /v1/insurance/*     -> INSURANCE_URL
//   /v1/inspection/*    -> INSPECTION_URL
//   /v1/notify          -> NOTIFICATIONS_URL
package main

import (
	"context"
	"net/http"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/gateway/internal/proxy"
)

func main() {
	cfg := config.MustLoad("gateway", 8080)
	log := logger.New(cfg.LogLevel)
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)

	h := proxy.New(log, issuer, proxy.RoutesFromEnv())
	if err := server.Run(context.Background(), log, cfg.Port, h); err != nil {
		log.Error("gateway exited", "err", err)
	}

	_ = http.ErrServerClosed
}
