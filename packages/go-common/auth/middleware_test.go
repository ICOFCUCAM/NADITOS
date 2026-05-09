package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/auth"
)

// helper to mint a token for the supplied claims.
func mint(t *testing.T, iss *auth.Issuer, c auth.Claims) string {
	t.Helper()
	tok, err := iss.Sign(uuid.MustParse("11111111-1111-1111-1111-111111111111"), c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// passthrough handler that 200s and lets us inspect ClaimsFrom(ctx).
func ok(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	if c != nil {
		w.Header().Set("X-Test-Tenant", c.TenantID)
		w.Header().Set("X-Test-Role", c.Role)
	}
	w.WriteHeader(http.StatusOK)
}

// TestMiddleware_NoBearer_401: missing Authorization header → 401.
func TestMiddleware_NoBearer_401(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	rec := httptest.NewRecorder()
	iss.Middleware(http.HandlerFunc(ok)).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer: %d", rec.Code)
	}
}

// TestMiddleware_BadToken_401: garbage in the bearer slot → 401, never
// reaches the inner handler.
func TestMiddleware_BadToken_401(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer not-a-jwt")
	rec := httptest.NewRecorder()
	iss.Middleware(http.HandlerFunc(ok)).ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", rec.Code)
	}
}

// TestMiddleware_ValidToken_BindsClaims: a valid token passes through
// and the inner handler sees the claims.
func TestMiddleware_ValidToken_BindsClaims(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	tok := mint(t, iss, auth.Claims{TenantID: "demo", Role: "admin"})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	iss.Middleware(http.HandlerFunc(ok)).ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: %d", rec.Code)
	}
	if rec.Header().Get("X-Test-Tenant") != "demo" {
		t.Fatalf("tenant didn't bind: %q", rec.Header().Get("X-Test-Tenant"))
	}
}

// TestMiddleware_TenantMismatch_403: a request that supplies an
// X-Tenant-Id header that doesn't match the JWT tenant is rejected
// even with a valid token. Defence against cross-tenant token replay.
func TestMiddleware_TenantMismatch_403(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	tok := mint(t, iss, auth.Claims{TenantID: "demo"})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("X-Tenant-Id", "other-tenant")
	rec := httptest.NewRecorder()
	iss.Middleware(http.HandlerFunc(ok)).ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d", rec.Code)
	}
}

// TestRequirePermission_HasIt: the inner handler runs when the JWT
// carries the named permission.
func TestRequirePermission_HasIt(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	tok := mint(t, iss, auth.Claims{TenantID: "demo", Permissions: []string{"fines:read"}})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	iss.Middleware(auth.RequirePermission("fines:read")(http.HandlerFunc(ok))).ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("perm present: %d %s", rec.Code, rec.Body.String())
	}
}

// TestRequirePermission_LacksIt_403: missing permission → 403.
func TestRequirePermission_LacksIt_403(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	tok := mint(t, iss, auth.Claims{TenantID: "demo"})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	iss.Middleware(auth.RequirePermission("fines:read")(http.HandlerFunc(ok))).ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("perm missing: %d", rec.Code)
	}
}

// TestRequireAnyRole_OneMatches: a JWT with role="officer" passes
// RequireAnyRole("admin","officer") because at least one matches.
func TestRequireAnyRole_OneMatches(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	tok := mint(t, iss, auth.Claims{TenantID: "demo", Role: "officer"})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	iss.Middleware(auth.RequireAnyRole("admin", "officer")(http.HandlerFunc(ok))).ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("any-role match: %d", rec.Code)
	}
}

// TestRequireAnyRole_NoneMatch: a citizen JWT doesn't satisfy
// admin/officer → 403.
func TestRequireAnyRole_NoneMatch(t *testing.T) {
	iss := auth.NewIssuer(testSecret, time.Minute, time.Hour)
	tok := mint(t, iss, auth.Claims{TenantID: "demo", Role: "citizen"})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	iss.Middleware(auth.RequireAnyRole("admin", "officer")(http.HandlerFunc(ok))).ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("any-role miss: %d", rec.Code)
	}
}

// TestPassword_RoundTrip: HashPassword + CheckPassword agrees on the
// right password and rejects a wrong one. bcrypt is slow on purpose,
// so this test takes a couple of seconds.
func TestPassword_RoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("the-correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.CheckPassword(hash, "the-correct-password"); err != nil {
		t.Fatalf("right pw rejected: %v", err)
	}
	if err := auth.CheckPassword(hash, "wrong-password"); err == nil {
		t.Fatal("wrong pw accepted")
	}
}
