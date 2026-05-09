package proxy_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/services/gateway/internal/proxy"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// upstream returns a handler that records the path it received.
type upstream struct {
	name string
	last string
}

func (u *upstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u.last = r.URL.Path
		w.Header().Set("X-Upstream", u.name)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok from " + u.name))
	}
}

// build wires the gateway with five test upstreams, then returns the
// handler plus pointers to each upstream so tests can assert which
// one received the request.
func build(t *testing.T) (http.Handler, *auth.Issuer,
	*upstream, *upstream, *upstream, *upstream, *upstream) {
	t.Helper()
	auth1 := &upstream{name: "auth"}
	registry := &upstream{name: "registry"}
	license := &upstream{name: "license"}
	fines := &upstream{name: "fines"}
	audit := &upstream{name: "audit"}
	authSrv := httptest.NewServer(auth1.handler())
	regSrv := httptest.NewServer(registry.handler())
	licSrv := httptest.NewServer(license.handler())
	finSrv := httptest.NewServer(fines.handler())
	audSrv := httptest.NewServer(audit.handler())
	t.Cleanup(func() {
		authSrv.Close(); regSrv.Close(); licSrv.Close(); finSrv.Close(); audSrv.Close()
	})

	issuer := auth.NewIssuer("gateway-test-secret-do-not-use-in-prod-32-bytes!", time.Minute, time.Hour)
	routes := []proxy.Route{
		{Prefix: "/v1/auth/me",                     Upstream: authSrv.URL,  NeedsAuth: true},
		{Prefix: "/v1/fines/payments/webhooks/",    Upstream: finSrv.URL,   NeedsAuth: false},
		{Prefix: "/v1/citizens/me/license",         Upstream: licSrv.URL,   NeedsAuth: true},
		{Prefix: "/v1/citizens/me/owner",           Upstream: regSrv.URL,   NeedsAuth: true},
		{Prefix: "/v1/citizens/me/vehicles",        Upstream: regSrv.URL,   NeedsAuth: true},
		{Prefix: "/v1/citizens/me/transfers",       Upstream: regSrv.URL,   NeedsAuth: true},
		{Prefix: "/v1/owners",                      Upstream: regSrv.URL,   NeedsAuth: true, NeedsRole: "admin"},
		{Prefix: "/v1/vehicles",                    Upstream: regSrv.URL,   NeedsAuth: true},
		{Prefix: "/v1/fines",                       Upstream: finSrv.URL,   NeedsAuth: true},
		{Prefix: "/v1/audit",                       Upstream: audSrv.URL,   NeedsAuth: true, NeedsRole: "admin"},
	}
	return proxy.New(discardLogger(), issuer, routes), issuer, auth1, registry, license, fines, audit
}

func mintToken(t *testing.T, issuer *auth.Issuer, role string) string {
	t.Helper()
	uid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tok, err := issuer.Sign(uid, auth.Claims{
		TenantID: "test", Role: role, Permissions: []string{"*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// TestRoute_Webhooks_BypassAuth: the payment webhook route must reach
// fines without a JWT — providers don't carry one. Critical because
// the same /v1/fines prefix would otherwise demand auth and 401 every
// real-world webhook.
func TestRoute_Webhooks_BypassAuth(t *testing.T) {
	h, _, _, _, _, fines, _ := build(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST",
		"/v1/fines/payments/webhooks/dev-stub", nil)
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if fines.last != "/v1/fines/payments/webhooks/dev-stub" {
		t.Fatalf("fines.last: %q", fines.last)
	}
}

// TestRoute_FinesAuthed_RequiresJWT: the same fines upstream demands
// auth on its non-webhook paths. A bare request must 401 without ever
// reaching the upstream.
func TestRoute_FinesAuthed_RequiresJWT(t *testing.T) {
	h, _, _, _, _, fines, _ := build(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/fines/mine", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d %s", rec.Code, rec.Body.String())
	}
	if fines.last != "" {
		t.Fatalf("upstream contacted on unauth: %q", fines.last)
	}
}

// TestRoute_LongestPrefixWins: /v1/citizens/me/license must reach the
// license upstream, not fall back to a generic /v1/citizens or
// /v1/citizens/me/* registry route. The route table puts both license
// and registry behind /v1/citizens/me/*; longest-prefix is what
// disambiguates.
func TestRoute_LongestPrefixWins(t *testing.T) {
	h, issuer, _, registry, license, _, _ := build(t)
	tok := mintToken(t, issuer, "citizen")
	r := httptest.NewRequest("GET", "/v1/citizens/me/license", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("X-Tenant-Id", "test")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if license.last != "/v1/citizens/me/license" {
		t.Fatalf("license should have received the request, got last=%q", license.last)
	}
	if registry.last != "" {
		t.Fatalf("registry incorrectly received the request: last=%q", registry.last)
	}
}

// TestRoute_AdminRoleEnforced: /v1/audit demands NeedsRole=admin. A
// citizen JWT must 403 even with a valid bearer.
func TestRoute_AdminRoleEnforced(t *testing.T) {
	h, issuer, _, _, _, _, audit := build(t)
	tok := mintToken(t, issuer, "citizen")
	r := httptest.NewRequest("GET", "/v1/audit/events?tenant_id=test", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("X-Tenant-Id", "test")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %s", rec.Code, rec.Body.String())
	}
	if audit.last != "" {
		t.Fatalf("audit upstream contacted despite role gate: %q", audit.last)
	}
}

// TestRoute_AdminRolePassesAdmin: same path with an admin JWT goes
// through. Symmetric with the citizen-403 test above.
func TestRoute_AdminRolePassesAdmin(t *testing.T) {
	h, issuer, _, _, _, _, audit := build(t)
	tok := mintToken(t, issuer, "admin")
	r := httptest.NewRequest("GET", "/v1/audit/events?tenant_id=test", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("X-Tenant-Id", "test")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if audit.last == "" {
		t.Fatal("audit upstream should have received the request")
	}
}

// TestRoute_TenantHeaderMismatch: when a caller supplies an X-Tenant-Id
// that doesn't match their JWT, the gateway must 403 — defence
// against a token from one tenant being replayed against another.
func TestRoute_TenantHeaderMismatch(t *testing.T) {
	h, issuer, _, registry, _, _, _ := build(t)
	tok := mintToken(t, issuer, "citizen")
	r := httptest.NewRequest("GET", "/v1/citizens/me/owner", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("X-Tenant-Id", "wrong-tenant")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %s", rec.Code, rec.Body.String())
	}
	if registry.last != "" {
		t.Fatalf("upstream contacted despite tenant mismatch: %q", registry.last)
	}
}

// TestRoute_UnknownPath_404: prefixes not in the table must 404 at
// the gateway, never punching through to a default upstream.
func TestRoute_UnknownPath_404(t *testing.T) {
	h, _, _, _, _, _, _ := build(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/nothing-here", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d %s", rec.Code, rec.Body.String())
	}
}
