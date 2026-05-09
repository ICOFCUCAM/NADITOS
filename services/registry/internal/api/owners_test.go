package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	commonAudit "github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/registry/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func build(env *testkit.Env) http.Handler {
	return api.New(env.Cfg, discardLogger(),
		env.Pool, env.Issuer,
		commonAudit.New("", "registry"),
		events.NewInProc(discardLogger()),
	)
}

// Idempotently grant 'owners:self' to the citizen role for the test
// tenant. The 0006 migration grants it for the demo tenant; per-test
// tenants from testkit need it explicitly.
func grantOwnersSelf(t *testing.T, env *testkit.Env) {
	t.Helper()
	env.Exec(`INSERT INTO role_permissions (tenant_id, role_code, permission)
	         VALUES ($1, 'citizen', 'owners:self')
	         ON CONFLICT DO NOTHING`, env.Tenant)
}

// ─── Admin owner CRUD ──────────────────────────────────────────────────────
func TestOwners_AdminCreate_AndLookup(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("admin", "registry:read", "registry:write")

	body := `{"full_name":"Alice Citizen","email":"alice@example.com","phone":"+47-1234"}`
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/owners", body, tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/owners/"+created.ID, "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Alice Citizen") ||
		!strings.Contains(rec.Body.String(), "alice@example.com") {
		t.Fatalf("missing data: %s", rec.Body.String())
	}
}

func TestOwners_AdminSearch(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("admin", "registry:read", "registry:write")

	for _, name := range []string{"Alice", "Bob", "Alpha"} {
		body := fmt.Sprintf(`{"full_name":%q,"email":"%s@x"}`, name, strings.ToLower(name))
		rec := httptest.NewRecorder()
		build(env).ServeHTTP(rec, env.Req("POST", "/v1/owners", body, tok))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/owners?q=Al", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct{ FullName string `json:"full_name"` } `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 2 { // Alice + Alpha
		t.Fatalf("want 2 matches for Al, got %d", len(out.Items))
	}
}

// ─── Vehicle linking ───────────────────────────────────────────────────────
func TestOwners_LinkVehicle_Transfer(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("admin", "registry:read", "registry:write")
	h := build(env)

	// Create owner A, owner B, a vehicle.
	a := postID(t, h, env, tok, "/v1/owners", `{"full_name":"Owner A"}`)
	b := postID(t, h, env, tok, "/v1/owners", `{"full_name":"Owner B"}`)
	vid := postID(t, h, env, tok, "/v1/vehicles",
		fmt.Sprintf(`{"plate":"OWN-%s","owner_id":"%s"}`, uuid.NewString()[:6], a))

	// Link to B; vehicle.owner_id should now be B.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST", "/v1/owners/"+b+"/vehicles/"+vid, "", tok))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}

	var ownerID string
	if err := env.QueryRow(`SELECT owner_id::text FROM vehicles WHERE id=$1`, vid).
		Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerID != b {
		t.Fatalf("transfer failed: vehicle.owner_id=%s want %s", ownerID, b)
	}
}

// ─── Citizen self-claim ────────────────────────────────────────────────────
// TestMyVehicles_ResponseShape pins the GET /v1/citizens/me/vehicles
// envelope: { items: Vehicle[] } where each Vehicle includes the
// status field the citizen page renders. Catches a refactor that
// would silently break the citizen vehicles list.
func TestMyVehicles_ResponseShape(t *testing.T) {
	env := testkit.Setup(t)
	grantOwnersSelf(t, env)
	tok, citizenID := env.Token("citizen", "owners:self")
	h := build(env)

	// Citizen claims, admin creates a vehicle and links it.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST", "/v1/citizens/me/owner",
		`{"full_name":"Owner X","email":"x@example.com"}`, tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}
	var claimed struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &claimed)

	// Insert vehicle directly + link to owner.
	plate := "MV-" + uuid.NewString()[:6]
	env.Exec(`INSERT INTO vehicles (tenant_id, plate, owner_id)
	          VALUES ($1, $2, $3::uuid)`,
		env.Tenant, plate, claimed.ID)

	// Confirm via GET.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/vehicles", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID     string `json:"id"`
			Plate  string `json:"plate"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items: want 1, got %d", len(resp.Items))
	}
	if resp.Items[0].Plate != plate {
		t.Errorf("plate: %s", resp.Items[0].Plate)
	}
	if resp.Items[0].Status == "" {
		t.Error("status empty (v_vehicle_status join didn't populate)")
	}
	_ = citizenID // used implicitly via env.Token's user-row insert
}

// TestMyVehicles_StripsIsWanted: is_wanted is an operational marker
// (active warrant, ANPR alert pipeline). The registered owner must
// not see it via the citizen endpoint, even when set in the
// database — surfacing it would defeat its purpose.
func TestMyVehicles_StripsIsWanted(t *testing.T) {
	env := testkit.Setup(t)
	grantOwnersSelf(t, env)
	tok, _ := env.Token("citizen", "owners:self")
	h := build(env)

	// Claim owner.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST", "/v1/citizens/me/owner",
		`{"full_name":"Wanted Owner","email":"w@example.com"}`, tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}
	var claimed struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &claimed)

	// Vehicle linked to owner WITH is_wanted=true and is_stolen=true.
	// The stolen flag should still come through (the citizen reported
	// it). Only is_wanted is sanitised.
	plate := "MW-" + uuid.NewString()[:6]
	env.Exec(`INSERT INTO vehicles
	            (tenant_id, plate, owner_id, is_wanted, is_stolen)
	          VALUES ($1, $2, $3::uuid, true, true)`,
		env.Tenant, plate, claimed.ID)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/vehicles", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			Plate    string `json:"plate"`
			IsWanted bool   `json:"is_wanted"`
			IsStolen bool   `json:"is_stolen"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items: want 1, got %d", len(resp.Items))
	}
	if resp.Items[0].IsWanted {
		t.Error("is_wanted leaked to the registered owner")
	}
	if !resp.Items[0].IsStolen {
		t.Error("is_stolen should still surface to the owner")
	}
}

// TestOwners_GetMyOwner_RoundTrip: claim → GET returns the same row.
// Drives the citizen profile UI's pre-populate path. Pre-claim, GET
// must 404 so the UI can render an empty form instead of waiting on
// stale state.
func TestOwners_GetMyOwner_RoundTrip(t *testing.T) {
	env := testkit.Setup(t)
	grantOwnersSelf(t, env)
	tok, _ := env.Token("citizen", "owners:self")
	h := build(env)

	// Pre-claim → 404.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/owner", "", tok))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("pre-claim GET: want 404, got %d %s", rec.Code, rec.Body.String())
	}

	// Claim.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST", "/v1/citizens/me/owner",
		`{"full_name":"Round Trip","email":"rt@example.com","phone":"+1-555"}`, tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}

	// GET returns the same fields.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/owner", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		FullName string  `json:"full_name"`
		Email    *string `json:"email"`
		Phone    *string `json:"phone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.FullName != "Round Trip" {
		t.Fatalf("full_name: %q", got.FullName)
	}
	if got.Email == nil || *got.Email != "rt@example.com" {
		t.Fatalf("email: %v", got.Email)
	}
}

func TestOwners_CitizenSelfClaim_Idempotent(t *testing.T) {
	env := testkit.Setup(t)
	grantOwnersSelf(t, env)
	tok, userID := env.Token("citizen", "owners:self")
	h := build(env)

	body := `{"full_name":"Me Citizen","email":"me@example.com"}`
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, env.Req("POST", "/v1/citizens/me/owner", body, tok))
		if rec.Code != http.StatusOK {
			t.Fatalf("self-claim %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}

	// Exactly one owners row for this user.
	var count int
	if err := env.QueryRow(
		`SELECT count(*) FROM owners WHERE tenant_id=$1 AND user_id=$2`,
		env.Tenant, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("self-claim should be idempotent; got %d rows", count)
	}
}

// TestOwners_CitizenSelfClaim_TenantBound: a citizen cannot claim
// ownership in another tenant — the JWT's tid is the tenant of record,
// the body cannot override.
func TestOwners_CitizenSelfClaim_NoUserIDFromBody(t *testing.T) {
	env := testkit.Setup(t)
	grantOwnersSelf(t, env)
	tok, userID := env.Token("citizen", "owners:self")
	h := build(env)

	otherUser := uuid.NewString()
	body := fmt.Sprintf(`{"full_name":"Mallory","user_id":"%s"}`, otherUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST", "/v1/citizens/me/owner", body, tok))
	// DisallowUnknownFields: the handler doesn't accept user_id in body.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 unknown field, got %d %s", rec.Code, rec.Body.String())
	}

	// Even so, no row was created against the attacker's user_id.
	var count int
	_ = env.QueryRow(`SELECT count(*) FROM owners WHERE user_id::text=$1`, otherUser).Scan(&count)
	if count != 0 {
		t.Fatalf("attacker user_id should not have an owners row, got %d", count)
	}
	// And the JWT subject got nothing yet either (the request was 400).
	_ = env.QueryRow(`SELECT count(*) FROM owners WHERE user_id=$1`, userID).Scan(&count)
	if count != 0 {
		t.Fatalf("400 path should not create a row, got %d", count)
	}
}

func TestOwners_CitizenSelfClaim_RequiresPerm(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("citizen") // no owners:self
	h := build(env)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST", "/v1/citizens/me/owner",
		`{"full_name":"Nope"}`, tok))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %s", rec.Code, rec.Body.String())
	}
}

// ─── Tenant isolation ──────────────────────────────────────────────────────
func TestOwners_TenantIsolation(t *testing.T) {
	envA := testkit.Setup(t)
	envB := testkit.Setup(t)
	tokA, _ := envA.Token("admin", "registry:read", "registry:write")
	tokB, _ := envB.Token("admin", "registry:read", "registry:write")
	hA := build(envA)
	hB := build(envB)

	postID(t, hA, envA, tokA, "/v1/owners", `{"full_name":"Tenant A Owner"}`)

	rec := httptest.NewRecorder()
	hB.ServeHTTP(rec, envB.Req("GET", "/v1/owners", "", tokB))
	if rec.Code != http.StatusOK {
		t.Fatalf("list B: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []any `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 0 {
		t.Fatalf("tenant B saw %d owners, expected 0", len(out.Items))
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────
func postID(t *testing.T, h http.Handler, env *testkit.Env, tok, path, body string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST", path, body, tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("post %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var out struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ID == "" {
		t.Fatalf("post %s: no id in %s", path, rec.Body.String())
	}
	return out.ID
}
