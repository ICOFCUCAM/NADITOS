// Package proxy implements the gateway request pipeline:
//
//   request-id -> structured log -> CORS -> rate-limit ->
//   optional JWT verify -> route match -> reverse proxy
package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type Gateway struct {
	log     *slog.Logger
	issuer  *auth.Issuer
	routes  []routeWithProxy
	limiter *rateLimiter
}

type routeWithProxy struct {
	Route
	rev *httputil.ReverseProxy
}

func New(log *slog.Logger, issuer *auth.Issuer, routes []Route) http.Handler {
	g := &Gateway{
		log:     log,
		issuer:  issuer,
		limiter: newRateLimiter(),
	}
	for _, r := range routes {
		u, err := url.Parse(r.Upstream)
		if err != nil {
			log.Error("invalid upstream URL, skipping", "prefix", r.Prefix, "url", r.Upstream)
			continue
		}
		g.routes = append(g.routes, routeWithProxy{Route: r, rev: httputil.NewSingleHostReverseProxy(u)})
	}
	return g
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rid := r.Header.Get("X-Request-Id")
	if rid == "" {
		rid = uuid.NewString()
	}
	w.Header().Set("X-Request-Id", rid)

	// CORS for browser-based callers (admin / police / citizen apps).
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Tenant-Id, X-Request-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	start := time.Now()
	defer func() {
		g.log.Info("gateway",
			slog.String("rid", rid),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("ip", clientIP(r)),
			slog.Duration("dur", time.Since(start)),
		)
	}()

	// Match route by prefix (longest-prefix wins).
	var match *routeWithProxy
	for i := range g.routes {
		rt := &g.routes[i]
		if strings.HasPrefix(r.URL.Path, rt.Prefix) {
			if match == nil || len(rt.Prefix) > len(match.Prefix) {
				match = rt
			}
		}
	}
	if match == nil {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}

	// Auth — verify JWT once at the edge for protected routes.
	tenant := r.Header.Get("X-Tenant-Id")
	if match.NeedsAuth {
		tok := auth.BearerToken(r)
		if tok == "" {
			httpx.WriteErr(w, httpx.ErrUnauthorized)
			return
		}
		c, err := g.issuer.Parse(tok)
		if err != nil {
			httpx.WriteErr(w, httpx.ErrUnauthorized)
			return
		}
		if tenant == "" {
			tenant = c.TenantID
			r.Header.Set("X-Tenant-Id", tenant)
		} else if tenant != c.TenantID {
			httpx.WriteErr(w, httpx.ErrForbidden)
			return
		}
		if match.NeedsRole != "" && c.Role != match.NeedsRole {
			httpx.WriteErr(w, httpx.ErrForbidden)
			return
		}
	}

	// Rate limit per (tenant, prefix) when configured.
	if match.RateLimit > 0 {
		if !g.limiter.Allow(tenant, match.Prefix, match.RateLimit) {
			http.Error(w, `{"code":"rate_limited"}`, http.StatusTooManyRequests)
			return
		}
	}

	// Forward.
	r.Header.Set("X-Forwarded-For", clientIP(r))
	r.Header.Set("X-Request-Id", rid)
	match.rev.ServeHTTP(w, r)
}

func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if i := strings.Index(h, ","); i > 0 {
			return strings.TrimSpace(h[:i])
		}
		return strings.TrimSpace(h)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}
