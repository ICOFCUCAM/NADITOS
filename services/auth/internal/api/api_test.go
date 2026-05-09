package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/auth/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// build wires the auth API against the test environment. Auth runs
// across all tenants (login establishes one), so it uses the admin
// pool — same as the runtime configuration.
func build(env *testkit.Env) http.Handler {
	return api.New(env.Cfg, discardLogger(), env.AdminPool())
}

// seedUser creates a user via /v1/admin/users so we get the same
// password-hash + role-link path the smoke uses. The admin endpoint is
// gated by ADMIN_BOOTSTRAP_KEY or an admin JWT; tests use the bootstrap
// key path because there's no admin user to log in as yet.
const testBootstrapKey = "test-bootstrap-key"

func seedUser(t *testing.T, env *testkit.Env, h http.Handler, email, password, role string) {
	t.Helper()
	t.Setenv("ADMIN_BOOTSTRAP_KEY", testBootstrapKey)
	body := `{"email":"` + email + `","password":"` + password +
		`","full_name":"Test","roles":["` + role + `"]}`
	r := httptest.NewRequest("POST", "/v1/admin/users", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-Id", env.Tenant)
	r.Header.Set("X-Admin-Bootstrap-Key", testBootstrapKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code/100 != 2 {
		t.Fatalf("seed user %s: %d %s", email, rec.Code, rec.Body.String())
	}
}

func login(t *testing.T, env *testkit.Env, h http.Handler, email, password string) *httptest.ResponseRecorder {
	body := `{"email":"` + email + `","password":"` + password + `"}`
	r := httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// TestLogin_HappyPath: creating a user then logging in returns 200
// + access_token + refresh_token, and the JWT carries the right
// tenant + role.
func TestLogin_HappyPath(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	seedUser(t, env, h, "alice@example.com", "secret123", "admin")

	rec := login(t, env, h, "alice@example.com", "secret123")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", out)
	}
	issuer := auth.NewIssuer(env.Cfg.JWTSecret, env.Cfg.AccessTTL, env.Cfg.RefreshTTL)
	c, err := issuer.Parse(out.AccessToken)
	if err != nil {
		t.Fatalf("JWT parse: %v", err)
	}
	if c.TenantID != env.Tenant {
		t.Fatalf("tenant: want %s, got %s", env.Tenant, c.TenantID)
	}
	if c.Role != "admin" {
		t.Fatalf("role: want admin, got %s", c.Role)
	}
}

// TestLogin_WrongPassword_401: an existing user with a wrong password
// gets a generic 401 — same shape as unknown email so we don't leak
// whether an account exists.
func TestLogin_WrongPassword_401(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	seedUser(t, env, h, "bob@example.com", "right-password", "citizen")

	rec := login(t, env, h, "bob@example.com", "wrong-password")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestLogin_UnknownUser_401: same 401 for an account that doesn't
// exist. Email-existence oracle protection.
func TestLogin_UnknownUser_401(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	rec := login(t, env, h, "ghost@example.com", "anything")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user: want 401, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestLogin_InactiveUser_401: a deactivated account (is_active=false)
// can't log in even with the right password. Critical for offboarding
// flows where we want the JWT path closed but the row preserved for
// audit.
func TestLogin_InactiveUser_401(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	seedUser(t, env, h, "cara@example.com", "pw1234", "citizen")
	env.Exec(`UPDATE users SET is_active=false WHERE email=$1 AND tenant_id=$2`,
		"cara@example.com", env.Tenant)

	rec := login(t, env, h, "cara@example.com", "pw1234")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("inactive: want 401, got %d %s", rec.Code, rec.Body.String())
	}
}

// loginGetTokens runs login and parses out access + refresh tokens.
// Returns "", "" if login fails (test helper, t.Fatal-friendly).
func loginGetTokens(t *testing.T, env *testkit.Env, h http.Handler, email, pw string) (string, string) {
	t.Helper()
	rec := login(t, env, h, email, pw)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.AccessToken, out.RefreshToken
}

// TestRefresh_HappyPath: a valid refresh token mints a fresh access
// token. The new access token parses + carries the right tenant.
func TestRefresh_HappyPath(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	seedUser(t, env, h, "ref@example.com", "secret123", "admin")
	_, refresh := loginGetTokens(t, env, h, "ref@example.com", "secret123")

	r := httptest.NewRequest("POST", "/v1/auth/refresh", strings.NewReader(
		`{"refresh_token":"`+refresh+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ AccessToken string `json:"access_token"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.AccessToken == "" {
		t.Fatal("refresh returned empty access token")
	}
}

// TestRefresh_BogusToken_401: a token that doesn't match any
// refresh_tokens row returns 401, not 500.
func TestRefresh_BogusToken_401(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	r := httptest.NewRequest("POST", "/v1/auth/refresh", strings.NewReader(
		`{"refresh_token":"bogus"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bogus refresh: %d", rec.Code)
	}
}

// TestRefresh_AfterLogout_401: logout revokes the refresh row, so a
// subsequent refresh with the same token must fail.
func TestRefresh_AfterLogout_401(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	seedUser(t, env, h, "lo@example.com", "pw1234", "citizen")
	_, refresh := loginGetTokens(t, env, h, "lo@example.com", "pw1234")

	// Logout.
	r := httptest.NewRequest("POST", "/v1/auth/logout", strings.NewReader(
		`{"refresh_token":"`+refresh+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body.String())
	}

	// Refresh should now 401.
	r = httptest.NewRequest("POST", "/v1/auth/refresh", strings.NewReader(
		`{"refresh_token":"`+refresh+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: %d", rec.Code)
	}
}

// TestMe_ReturnsClaims: GET /v1/auth/me with a valid bearer returns
// the JWT claims (id, tenant, role). No bearer → 401, bad bearer → 401.
func TestMe_ReturnsClaims(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	seedUser(t, env, h, "me@example.com", "pw1234", "officer")
	access, _ := loginGetTokens(t, env, h, "me@example.com", "pw1234")

	r := httptest.NewRequest("GET", "/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer "+access)
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Tenant string `json:"tenant"`
		Role   string `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Tenant != env.Tenant || out.Role != "officer" {
		t.Fatalf("claims: %+v", out)
	}

	// No bearer.
	r = httptest.NewRequest("GET", "/v1/auth/me", nil)
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer: %d", rec.Code)
	}

	// Bad bearer.
	r = httptest.NewRequest("GET", "/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer not-a-jwt")
	r.Header.Set("X-Tenant-Id", env.Tenant)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad bearer: %d", rec.Code)
	}
}

// TestLogin_TenantIsolation: the same email registered in two tenants
// must resolve to the right user based on the X-Tenant-Id header,
// not collapse into one. RLS isn't enough here — login crosses tenants
// to look up the row, so the WHERE clause carries the responsibility.
func TestLogin_TenantIsolation(t *testing.T) {
	envA := testkit.Setup(t)
	envB := testkit.Setup(t)
	hA := build(envA)
	hB := build(envB)

	seedUser(t, envA, hA, "shared@example.com", "pw-a", "admin")
	seedUser(t, envB, hB, "shared@example.com", "pw-b", "citizen")

	// Tenant A's password works against A but not B.
	if rec := login(t, envA, hA, "shared@example.com", "pw-a"); rec.Code != http.StatusOK {
		t.Fatalf("A login with A pw: %d %s", rec.Code, rec.Body.String())
	}
	if rec := login(t, envB, hB, "shared@example.com", "pw-a"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("B login with A pw: %d %s", rec.Code, rec.Body.String())
	}
	if rec := login(t, envB, hB, "shared@example.com", "pw-b"); rec.Code != http.StatusOK {
		t.Fatalf("B login with B pw: %d %s", rec.Code, rec.Body.String())
	}
}
