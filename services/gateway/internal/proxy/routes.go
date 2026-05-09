package proxy

import "os"

// Route binds a URL prefix to an upstream service URL. The first match wins.
type Route struct {
	Prefix      string // e.g. "/v1/vehicles"
	Upstream    string // e.g. "http://registry:8002"
	NeedsAuth   bool
	NeedsRole   string // empty = any authenticated user
	RateLimit   int    // req/min/tenant; 0 = unlimited
}

// RoutesFromEnv builds the default route table from environment variables.
// Override individual routes with GATEWAY_ROUTE_<NAME>=url.
func RoutesFromEnv() []Route {
	auth := getEnv("AUTH_URL",          "http://auth:8001")
	registry := getEnv("REGISTRY_URL",  "http://registry:8002")
	license := getEnv("LICENSE_URL",    "http://license:8003")
	insurance := getEnv("INSURANCE_URL","http://insurance:8004")
	inspection := getEnv("INSPECTION_URL","http://inspection:8005")
	fines := getEnv("FINES_URL",        "http://fines:8006")
	audit := getEnv("AUDIT_URL",        "http://audit:8007")
	anpr := getEnv("ANPR_URL",          "http://anpr-gateway:8008")
	notify := getEnv("NOTIFICATIONS_URL","http://notifications:8009")

	return []Route{
		{Prefix: "/v1/auth/login",    Upstream: auth, NeedsAuth: false, RateLimit: 60},
		{Prefix: "/v1/auth/refresh",  Upstream: auth, NeedsAuth: false, RateLimit: 120},
		{Prefix: "/v1/auth/logout",   Upstream: auth, NeedsAuth: false, RateLimit: 60},
		{Prefix: "/v1/auth/me",       Upstream: auth, NeedsAuth: true},
		{Prefix: "/v1/admin/users",   Upstream: auth, NeedsAuth: true, NeedsRole: "admin"},

		// Provider webhooks are signature-authenticated, never JWT — and
		// providers won't send us a tenant header. Match BEFORE the
		// authenticated /v1/fines route so the gateway doesn't reject
		// them at the JWT gate.
		{Prefix: "/v1/fines/payments/webhooks/", Upstream: fines, NeedsAuth: false, RateLimit: 600},

		// Citizen self-service. Each /v1/citizens/me/* path is owned by
		// a different upstream. The gateway resolves by longest-prefix
		// match, so specific routes here win against any future
		// /v1/citizens fallback regardless of declaration order.
		{Prefix: "/v1/citizens/me/license",       Upstream: license,  NeedsAuth: true},
		{Prefix: "/v1/citizens/me/notifications", Upstream: notify,   NeedsAuth: true},
		{Prefix: "/v1/citizens/me/owner",         Upstream: registry, NeedsAuth: true},
		{Prefix: "/v1/citizens/me/vehicles",      Upstream: registry, NeedsAuth: true},
		{Prefix: "/v1/citizens/me/transfers",     Upstream: registry, NeedsAuth: true},

		// Owners (admin) live in registry alongside vehicles.
		{Prefix: "/v1/owners",        Upstream: registry, NeedsAuth: true, NeedsRole: "admin"},

		{Prefix: "/v1/vehicles",      Upstream: registry, NeedsAuth: true, RateLimit: 600},
		{Prefix: "/v1/fines",         Upstream: fines,    NeedsAuth: true, RateLimit: 600},
		// Officer self-stats live on a sub-prefix that's NOT admin-gated
		// so officers can view their own activity. Must come before
		// /v1/audit's admin gate by being a longer prefix; longest-
		// prefix-wins handles that automatically.
		{Prefix: "/v1/audit/officers/me", Upstream: audit, NeedsAuth: true},
		{Prefix: "/v1/audit",         Upstream: audit,    NeedsAuth: true, NeedsRole: "admin"},
		{Prefix: "/v1/licenses",      Upstream: license,  NeedsAuth: true},
		{Prefix: "/v1/insurance",     Upstream: insurance, NeedsAuth: true},
		{Prefix: "/v1/inspection",    Upstream: inspection, NeedsAuth: true},
		{Prefix: "/v1/anpr",          Upstream: anpr,     NeedsAuth: true, NeedsRole: "officer"},
		{Prefix: "/v1/notify",        Upstream: notify,   NeedsAuth: true, NeedsRole: "admin"},
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
