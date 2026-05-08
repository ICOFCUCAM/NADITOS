package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/audit/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func build(env *testkit.Env) http.Handler {
	// The audit service is the one component that writes across tenants
	// (it stamps every other service's mutations) and runs with a
	// privileged DB role in production. Tests use the admin pool to
	// match that operational shape.
	return api.New(env.Cfg, discardLogger(), env.AdminPool())
}

func writeEvent(t *testing.T, h http.Handler, env *testkit.Env, action, resource, resourceID string) string {
	t.Helper()
	body := `{
		"tenant_id": "` + env.Tenant + `",
		"service": "test",
		"action": "` + action + `",
		"resource_type": "` + resource + `",
		"resource_id": "` + resourceID + `"
	}`
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/audit/events", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("write event %q: %d %s", action, rec.Code, rec.Body.String())
	}
	var out struct {
		ID   int64  `json:"id"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Hash
}

// TestAudit_AppendOnly ensures the database trigger blocks UPDATE and
// DELETE on audit_events — the immutability guarantee can't be undone
// even by a privileged DB user.
func TestAudit_AppendOnly(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)
	writeEvent(t, h, env, "test.create", "thing", "thing-1")

	// Try UPDATE — should raise audit_immutable.
	if _, err := env.AdminExec("UPDATE audit_events SET action='changed' WHERE tenant_id=$1", env.Tenant); err == nil {
		t.Fatal("expected UPDATE to fail (audit_events should be append-only)")
	}
	// Try DELETE — should also raise.
	if _, err := env.AdminExec("DELETE FROM audit_events WHERE tenant_id=$1", env.Tenant); err == nil {
		t.Fatal("expected DELETE to fail (audit_events should be append-only)")
	}
}

// TestAudit_HashChain confirms each new event hashes prev_hash || canon
// of the row, and the /verify endpoint reports a valid chain.
func TestAudit_HashChain(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)

	for i := 0; i < 5; i++ {
		writeEvent(t, h, env, "test.add", "thing", "thing-id")
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/verify?tenant_id="+env.Tenant, nil)
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK      bool `json:"ok"`
		Checked int  `json:"checked"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.OK || out.Checked != 5 {
		t.Fatalf("want ok=true checked=5, got %+v", out)
	}
}

// TestAudit_ChainBreakDetected: tamper one row's hash directly via the
// admin pool (bypassing the trigger via a TRUNCATE+RE-INSERT path is the
// only way to defeat append-only in practice; here we simulate a
// post-write-time tamper by altering the trigger temporarily). The
// verifier MUST detect the break.
func TestAudit_ChainBreakDetected(t *testing.T) {
	env := testkit.Setup(t)
	h := build(env)

	for i := 0; i < 3; i++ {
		writeEvent(t, h, env, "test.add", "thing", "thing-id")
	}

	// Disable the trigger ONLY for this test, mutate the middle row's
	// hash, then re-enable. This is what an attacker with DB access
	// would have to do — and we want the verifier to catch it.
	if _, err := env.AdminExec(`ALTER TABLE audit_events DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.AdminExec(
		`UPDATE audit_events
		    SET hash = decode('00000000000000000000000000000000000000000000000000000000000000ff','hex')
		  WHERE tenant_id=$1
		    AND id = (SELECT MIN(id) FROM audit_events WHERE tenant_id=$1)`,
		env.Tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := env.AdminExec(`ALTER TABLE audit_events ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/verify?tenant_id="+env.Tenant, nil)
	h.ServeHTTP(rec, r)
	var out struct {
		OK        bool `json:"ok"`
		BrokenAt  int64 `json:"broken_at"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.OK {
		t.Fatalf("verifier did NOT detect tampered hash, body: %s", rec.Body.String())
	}
	if out.BrokenAt == 0 {
		t.Fatalf("verifier reports break but no broken_at id, body: %s", rec.Body.String())
	}
}

// TestAudit_TenantIsolation: events for tenant A are not visible when
// listing or verifying tenant B's chain.
func TestAudit_TenantIsolation(t *testing.T) {
	envA := testkit.Setup(t)
	envB := testkit.Setup(t)
	hA := build(envA)
	hB := build(envB)
	for i := 0; i < 3; i++ {
		writeEvent(t, hA, envA, "test.add", "thing", "a")
	}

	rec := httptest.NewRecorder()
	hB.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/audit/events?tenant_id="+envB.Tenant, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []any `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 0 {
		t.Fatalf("tenant B listed %d events, expected 0", len(out.Items))
	}
}
