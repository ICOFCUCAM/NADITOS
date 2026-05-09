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

// quiet unused-import warnings if any helper above goes unused after
// future trimming.
var _ = time.Time{}
