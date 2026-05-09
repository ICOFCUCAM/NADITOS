package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	commonAudit "github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/license/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func build(env *testkit.Env) http.Handler {
	return api.New(env.Cfg, discardLogger(), env.Pool, env.Issuer,
		commonAudit.New("", "license"),
		events.NewInProc(discardLogger()))
}

// seedLicenseForCitizen returns the citizen's user id, license id,
// and a JWT for that user. The license has a couple of demerit events
// and a (lifted) suspension so the my-license bundle has all the
// arrays populated for the shape test.
func seedLicenseForCitizen(t *testing.T, env *testkit.Env) (uid, lid string, tok string) {
	t.Helper()
	tok, uid = env.Token("citizen")
	licID := uuid.New()
	env.Exec(`INSERT INTO driver_licenses
	            (id, tenant_id, user_id, license_number,
	             full_name, classes, issued_at, expires_at, points)
	          VALUES ($1, $2, $3::uuid, $4, 'Test driver',
	                  ARRAY['B'], '2020-01-01', '2030-01-01', 6)`,
		licID, env.Tenant, uid, "DL-TEST-"+licID.String()[:6])
	env.Exec(`INSERT INTO driver_demerit_events
	            (tenant_id, license_id, delta, reason, source, occurred_at)
	          VALUES ($1, $2, 6, 'fine:SPEED_30', 'fine', now() - interval '7 days')`,
		env.Tenant, licID)
	env.Exec(`INSERT INTO driver_suspensions
	            (tenant_id, license_id, reason, trigger_kind,
	             starts_at, ends_at, lifted_at)
	          VALUES ($1, $2, 'demerit threshold', 'demerit',
	                  now() - interval '90 days', now() - interval '30 days',
	                  now() - interval '20 days')`,
		env.Tenant, licID)
	return uid, licID.String(), tok
}

// TestMyLicense_ResponseShape pins the {license, standing,
// recent_violations, demerits, suspensions} envelope the citizen
// /license page consumes. A backend refactor that flattens the
// response would silently break the page; this test catches it.
func TestMyLicense_ResponseShape(t *testing.T) {
	env := testkit.Setup(t)
	_, lid, tok := seedLicenseForCitizen(t, env)

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/license", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		License struct {
			ID            string `json:"id"`
			LicenseNumber string `json:"license_number"`
			Points        int    `json:"points"`
		} `json:"license"`
		Standing         string                   `json:"standing"`
		RecentViolations int                      `json:"recent_violations"`
		Demerits         []map[string]interface{} `json:"demerits"`
		Suspensions      []map[string]interface{} `json:"suspensions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.License.ID != lid {
		t.Errorf("license.id: want %s, got %s", lid, resp.License.ID)
	}
	if resp.License.Points != 6 {
		t.Errorf("license.points: want 6, got %d", resp.License.Points)
	}
	if resp.Standing == "" {
		t.Error("standing missing")
	}
	if len(resp.Demerits) != 1 {
		t.Errorf("demerits: want 1, got %d", len(resp.Demerits))
	}
	if len(resp.Suspensions) != 1 {
		t.Errorf("suspensions: want 1, got %d", len(resp.Suspensions))
	}
}

// TestMyLicense_NoLicense_404: a citizen with no driver_licenses row
// gets 404 so the UI can render an empty-state instead of crashing
// on a partial bundle.
func TestMyLicense_NoLicense_404(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("citizen")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/license", "", tok))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSuspension_LiftClearsCachedFlag: lifting the only active
// suspension on a license must clear is_suspended and
// suspended_until on driver_licenses, so v_driver_standing flips
// back to non-suspended on the next read. A regression here would
// leave a citizen showing as suspended after the admin lifts.
func TestSuspension_LiftClearsCachedFlag(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("admin", "license:read", "license:write")
	h := build(env)

	// Seed a license and an active suspension. is_suspended=true,
	// suspended_until in the future — i.e. the cached state.
	licID := uuid.New()
	end := time.Now().Add(60 * 24 * time.Hour).UTC()
	env.Exec(`INSERT INTO driver_licenses
	            (id, tenant_id, license_number, full_name, classes,
	             issued_at, expires_at, points,
	             is_suspended, suspended_until)
	          VALUES ($1, $2, $3, 'Test', ARRAY['B'],
	                  '2020-01-01', '2030-01-01', 6,
	                  true, $4::date)`,
		licID, env.Tenant, "LIFT-"+licID.String()[:6], end)
	var sid string
	env.QueryRow(`INSERT INTO driver_suspensions
	                (tenant_id, license_id, reason, trigger_kind,
	                 starts_at, ends_at)
	              VALUES ($1, $2, 'demerit threshold', 'demerit',
	                      now() - interval '5 days', $3)
	              RETURNING id::text`,
		env.Tenant, licID, end).Scan(&sid)

	// Sanity: standing reflects 'suspended' before the lift.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("GET", "/v1/licenses/"+licID.String()+"/standing", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("standing pre: %d %s", rec.Code, rec.Body.String())
	}
	var pre struct{ Standing string `json:"standing"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &pre)
	if pre.Standing != "suspended" {
		t.Fatalf("pre-lift standing: want suspended, got %q", pre.Standing)
	}

	// Lift via the admin endpoint.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/licenses/"+licID.String()+"/suspensions/"+sid+"/lift", "", tok))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("lift: %d %s", rec.Code, rec.Body.String())
	}

	// driver_licenses cached flag must be cleared.
	var isSusp bool
	var until *time.Time
	if err := env.QueryRow(
		`SELECT is_suspended, suspended_until FROM driver_licenses WHERE id=$1`,
		licID).Scan(&isSusp, &until); err != nil {
		t.Fatal(err)
	}
	if isSusp {
		t.Error("driver_licenses.is_suspended still true after lift")
	}
	if until != nil {
		t.Errorf("driver_licenses.suspended_until not cleared: %v", until)
	}

	// And the standing view flips to non-suspended.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("GET", "/v1/licenses/"+licID.String()+"/standing", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("standing post: %d %s", rec.Code, rec.Body.String())
	}
	var post struct{ Standing string `json:"standing"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &post)
	if post.Standing == "suspended" {
		t.Errorf("post-lift standing still suspended")
	}
}

// TestSuspension_LiftKeepsFlagWhenSecondActive: if a license has TWO
// active suspensions and the admin lifts ONE, is_suspended must
// stay true. Otherwise an admin who lifts a stale lower-stage
// suspension would accidentally reinstate driving privileges that
// a separate, still-valid suspension was supposed to deny.
func TestSuspension_LiftKeepsFlagWhenSecondActive(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("admin", "license:write", "license:read")
	h := build(env)

	licID := uuid.New()
	end := time.Now().Add(60 * 24 * time.Hour).UTC()
	env.Exec(`INSERT INTO driver_licenses
	            (id, tenant_id, license_number, full_name, classes,
	             issued_at, expires_at, points,
	             is_suspended, suspended_until)
	          VALUES ($1, $2, $3, 'Two', ARRAY['B'],
	                  '2020-01-01', '2030-01-01', 6,
	                  true, $4::date)`,
		licID, env.Tenant, "LIFT2-"+licID.String()[:6], end)
	var s1, s2 string
	if err := env.QueryRow(`INSERT INTO driver_suspensions
	                (tenant_id, license_id, reason, trigger_kind, starts_at, ends_at)
	              VALUES ($1, $2, 'first', 'demerit',
	                      now() - interval '20 days', $3)
	              RETURNING id::text`,
		env.Tenant, licID, end).Scan(&s1); err != nil {
		t.Fatalf("seed s1: %v", err)
	}
	if err := env.QueryRow(`INSERT INTO driver_suspensions
	                (tenant_id, license_id, reason, trigger_kind, starts_at, ends_at)
	              VALUES ($1, $2, 'second', 'administrative',
	                      now() - interval '5 days', $3)
	              RETURNING id::text`,
		env.Tenant, licID, end).Scan(&s2); err != nil {
		t.Fatalf("seed s2: %v", err)
	}

	// Lift the FIRST suspension only.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/licenses/"+licID.String()+"/suspensions/"+s1+"/lift", "", tok))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("lift: %d %s", rec.Code, rec.Body.String())
	}

	// is_suspended must remain TRUE because s2 is still active.
	var isSusp bool
	if err := env.QueryRow(
		`SELECT is_suspended FROM driver_licenses WHERE id=$1`, licID).Scan(&isSusp); err != nil {
		t.Fatal(err)
	}
	if !isSusp {
		t.Error("is_suspended cleared even though a second active suspension exists")
	}
	_ = s2
}

// quiet unused-import warnings if any helper above goes unused after
// future trimming.
var _ = time.Time{}
