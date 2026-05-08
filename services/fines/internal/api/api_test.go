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
	"time"

	"github.com/google/uuid"

	commonAudit "github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/contracts/payments"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/fines/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// build wires the fines API the same way main() does, against the
// per-test environment.
func build(env *testkit.Env) http.Handler {
	return api.New(env.Cfg, discardLogger(),
		env.Pool, env.Issuer,
		commonAudit.New("", "fines"), // empty URL → no-op audit emit
		payments.NewDevStub(),
		events.NewInProc(discardLogger()),
	)
}

func newVehicle(t *testing.T, env *testkit.Env) (vid uuid.UUID, plate string) {
	t.Helper()
	plate = fmt.Sprintf("AB-%s", strings.ToUpper(uuid.NewString()[:6]))
	vid = uuid.New()
	env.Exec(
		`INSERT INTO vehicles (id, tenant_id, plate, insurance_expires_at, inspection_expires_at)
		 VALUES ($1, $2, $3, now() - interval '1 day', now() - interval '1 day')`,
		vid, env.Tenant, plate)
	return
}

func validIssueBody(plate, offence string) string {
	return fmt.Sprintf(`{
		"plate": %q,
		"offence_code": %q,
		"geo_lat": 60.4,
		"geo_lng": 5.32,
		"geo_accuracy_m": 8,
		"device_id": "dev-test",
		"evidence": [{
			"kind": "photo",
			"s3_key": "evidence/test/x.jpg",
			"sha256": "%s",
			"bytes": 12345,
			"taken_at": %q
		}]
	}`, plate, offence, strings.Repeat("ab", 32), time.Now().UTC().Format(time.RFC3339))
}

// TestFineIssue_RequiresEvidence is the cornerstone anti-corruption rule:
// an officer cannot issue a fine without photographic evidence.
func TestFineIssue_RequiresEvidence(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	body := fmt.Sprintf(`{"plate": %q, "offence_code":"INS_EXPIRED",
		"geo_lat":60,"geo_lng":5,"device_id":"d","evidence":[]}`, plate)

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines", body, tok))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "evidence_required") {
		t.Fatalf("expected evidence_required code, got %s", rec.Body.String())
	}
}

// TestFineIssue_ServerPricedAmount verifies the officer cannot influence
// the fine amount — the server reads regulation_offences and ignores any
// `amount` field in the request.
func TestFineIssue_ServerPricedAmount(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	// Sneak in an "amount" field. The handler doesn't unmarshal it, but
	// even via DisallowUnknownFields it should be rejected — confirming
	// the contract.
	body := fmt.Sprintf(`{"plate":%q,"offence_code":"INS_EXPIRED","amount":"1.00",
		"geo_lat":60,"geo_lng":5,"device_id":"d","evidence":[
		{"kind":"photo","s3_key":"e","sha256":"abc","bytes":1,"taken_at":%q}]}`,
		plate, time.Now().UTC().Format(time.RFC3339))
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines", body, tok))
	if rec.Code == http.StatusCreated {
		t.Fatalf("officer-supplied amount should not be accepted: %s", rec.Body.String())
	}

	// Issue a normal fine and confirm the amount matches the catalog.
	rec = httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Amount != "400.00" || out.Currency != "EUR" {
		t.Fatalf("want 400.00 EUR (catalog), got %s %s", out.Amount, out.Currency)
	}
}

// TestFineIssue_DuplicateProtection: same vehicle+offence within the
// duplicate window returns 409, not a second fine.
func TestFineIssue_DuplicateProtection(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines", validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first issue: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines", validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: want 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "duplicate_fine") {
		t.Fatalf("expected duplicate_fine code, got %s", rec.Body.String())
	}
}

// TestFineIssue_UnknownOffence rejects invented codes — the officer
// cannot type one freely.
func TestFineIssue_UnknownOffence(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "MADE_UP_CODE"), tok))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown_offence") {
		t.Fatalf("expected unknown_offence code, got %s", rec.Body.String())
	}
}

// TestFineCancel_RequiresReason: an admin cancellation must be justified.
func TestFineCancel_RequiresReason(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tokOfficer, _ := env.Token("officer", "fines:create")
	tokAdmin, _ := env.Token("admin", "fines:cancel")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "INS_EXPIRED"), tokOfficer))
	var created struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines/"+created.ID+"/cancel", `{}`, tokAdmin))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cancel without reason: want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines/"+created.ID+"/cancel",
		`{"reason":"officer error: wrong vehicle"}`, tokAdmin))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cancel with reason: want 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify status flipped.
	var status string
	if err := env.QueryRow(`SELECT status::text FROM fines WHERE id=$1`, created.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("want status=cancelled, got %s", status)
	}
}

// TestFineIssue_NonOfficerForbidden: a citizen JWT cannot issue fines
// even with a tenant header — RBAC has the final say.
func TestFineIssue_NonOfficerForbidden(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("citizen") // no fines:create perm

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestFineIssue_TenantIsolation confirms a fine issued in tenant A is
// invisible to tenant B even with a valid JWT — RLS is the final gate.
func TestFineIssue_TenantIsolation(t *testing.T) {
	envA := testkit.Setup(t)
	envB := testkit.Setup(t)
	_, plateA := newVehicle(t, envA)

	tokA, _ := envA.Token("officer", "fines:create")
	rec := httptest.NewRecorder()
	build(envA).ServeHTTP(rec, envA.Req("POST", "/v1/fines",
		validIssueBody(plateA, "INS_EXPIRED"), tokA))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", rec.Code, rec.Body.String())
	}

	// Tenant B with fines:read should see ZERO fines — RLS isolates.
	tokB, _ := envB.Token("admin", "fines:read")
	rec = httptest.NewRecorder()
	build(envB).ServeHTTP(rec, envB.Req("GET", "/v1/fines", "", tokB))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []any `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 0 {
		t.Fatalf("tenant B saw %d fines, expected 0 (RLS leak!)", len(out.Items))
	}
}

// TestFineIssue_EvidenceMetadataRequired: each evidence item must carry
// s3_key, sha256, and taken_at. Without them the fine is rejected, so a
// fabricated "evidence" array of empty objects can't slip through.
func TestFineIssue_EvidenceMetadataRequired(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	body := fmt.Sprintf(`{"plate":%q,"offence_code":"INS_EXPIRED",
		"geo_lat":60,"geo_lng":5,"device_id":"d","evidence":[{"kind":"photo"}]}`, plate)
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines", body, tok))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "evidence_invalid") {
		t.Fatalf("expected evidence_invalid, got %s", rec.Body.String())
	}
}

// TestFineIssue_OfficerIdentityStamped: server records the JWT subject as
// issued_by — the officer cannot impersonate another officer.
func TestFineIssue_OfficerIdentityStamped(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, officerID := env.Token("officer", "fines:create")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", rec.Code, rec.Body.String())
	}

	var fid string
	_ = json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&struct {
		ID *string `json:"id"`
	}{ID: &fid})

	var dbOfficer string
	if err := env.QueryRow(`SELECT issued_by::text FROM fines WHERE id=$1`, fid).Scan(&dbOfficer); err != nil {
		t.Fatal(err)
	}
	if dbOfficer != officerID {
		t.Fatalf("issued_by stamped %s, expected JWT sub %s", dbOfficer, officerID)
	}
}

// TestFineIssue_NormalFlow_Persists confirms the happy path actually
// writes the fine + evidence rows transactionally.
func TestFineIssue_NormalFlow_Persists(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	var evidenceCount int
	if err := env.QueryRow(`SELECT count(*) FROM fine_evidence WHERE fine_id=$1`, out.ID).
		Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	if evidenceCount != 1 {
		t.Fatalf("want 1 evidence row, got %d", evidenceCount)
	}

	// And the audit / issuance metadata is captured.
	var deviceID string
	if err := env.QueryRow(`SELECT device_id FROM fines WHERE id=$1`, out.ID).Scan(&deviceID); err != nil {
		t.Fatal(err)
	}
	if deviceID != "dev-test" {
		t.Fatalf("device_id: want dev-test, got %s", deviceID)
	}
	// Suppress unused-time warning if the package gets pruned later.
	_ = time.Second
}

// TestFineIssue_OutboxAtomic confirms the fine-issued event lands in
// event_outbox in the same transaction as the fine + evidence rows.
// If the outbox INSERT failed independently, this test would not see
// the row at all.
func TestFineIssue_OutboxAtomic(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	// One row in event_outbox, undelivered, with type=fine.issued and
	// the matching fine_id in the payload. A relay process drains it.
	var (
		count       int
		envelopeRaw string
	)
	if err := env.QueryRow(
		`SELECT count(*) FROM event_outbox
		   WHERE tenant_id=$1 AND envelope->>'type'='fine.issued'
		     AND envelope->'data'->>'fine_id'=$2`,
		env.Tenant, out.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 outbox row for fine.issued, got %d", count)
	}
	if err := env.QueryRow(
		`SELECT envelope::text FROM event_outbox
		   WHERE tenant_id=$1 AND envelope->>'type'='fine.issued'
		     AND envelope->'data'->>'fine_id'=$2`,
		env.Tenant, out.ID).Scan(&envelopeRaw); err != nil {
		t.Fatal(err)
	}
	// pg JSONB output uses ": " separator; Contains skips whitespace.
	if !strings.Contains(envelopeRaw, `"actor_role"`) ||
		!strings.Contains(envelopeRaw, `"officer"`) {
		t.Fatalf("envelope missing actor_role=officer: %s", envelopeRaw)
	}

	// And the row is undelivered until the relay drains it.
	var delivered *time.Time
	if err := env.QueryRow(
		`SELECT delivered_at FROM event_outbox
		   WHERE tenant_id=$1 AND envelope->'data'->>'fine_id'=$2`,
		env.Tenant, out.ID).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if delivered != nil {
		t.Fatalf("expected undelivered, got %v", *delivered)
	}
}

// TestFineIssue_OutboxRollsBackOnFailure: when the duplicate-protection
// check rejects a request, the outbox row from the rejected attempt is
// also absent. (The first issue creates one outbox row; the second is
// 409 and must not double the count.)
func TestFineIssue_OutboxRollsBackOnFailure(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		rec := httptest.NewRecorder()
		build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
			validIssueBody(plate, "INS_EXPIRED"), tok))
		if rec.Code != want {
			t.Fatalf("issue %d: want %d, got %d: %s", i, want, rec.Code, rec.Body.String())
		}
	}

	var outboxCount int
	if err := env.QueryRow(
		`SELECT count(*) FROM event_outbox
		   WHERE tenant_id=$1 AND envelope->>'type'='fine.issued'`,
		env.Tenant).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 {
		t.Fatalf("want exactly 1 outbox row (duplicate must roll back), got %d", outboxCount)
	}
}

// seedDriverLicense creates a driver_licenses row for the given tenant
// and returns the license number. The smoke + this test rely on the
// fine handler resolving license_number → driver_license_id so the
// demerit engine can apply points.
func seedDriverLicense(t *testing.T, env *testkit.Env, fullName string) string {
	t.Helper()
	number := "DL-" + strings.ToUpper(uuid.NewString()[:8])
	env.Exec(`INSERT INTO driver_licenses
	         (tenant_id, license_number, full_name, classes,
	          issued_at, expires_at)
	         VALUES ($1, $2, $3, $4, '2020-01-01', '2030-01-01')`,
		env.Tenant, number, fullName, []string{"B"})
	return number
}

// TestFineIssue_AttachesDriverLicense: when the request includes the
// driver_license number, the fine row's driver_license_id is set so
// the demerit engine can pick it up downstream.
func TestFineIssue_AttachesDriverLicense(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	number := seedDriverLicense(t, env, "Driver Doe")
	tok, _ := env.Token("officer", "fines:create")

	body := fmt.Sprintf(`{
		"plate":%q,"offence_code":"INS_EXPIRED",
		"driver_license":%q,
		"geo_lat":60,"geo_lng":5,"device_id":"d","evidence":[
		{"kind":"photo","s3_key":"e","sha256":"abc","bytes":1,"taken_at":%q}]}`,
		plate, number, time.Now().UTC().Format(time.RFC3339))
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines", body, tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	// Cross-check that fines.driver_license_id matches the seeded one.
	var fineLID, expectedLID string
	if err := env.QueryRow(
		`SELECT driver_license_id::text FROM fines WHERE id=$1`, out.ID).
		Scan(&fineLID); err != nil {
		t.Fatal(err)
	}
	if err := env.QueryRow(
		`SELECT id::text FROM driver_licenses WHERE license_number=$1`, number).
		Scan(&expectedLID); err != nil {
		t.Fatal(err)
	}
	if fineLID != expectedLID {
		t.Fatalf("driver_license_id: want %s, got %s", expectedLID, fineLID)
	}
}

// TestFineIssue_UnknownDriverLicense_Rejects: a typo in the license
// number must be a 400, not a silently-stored fine with NULL
// driver_license_id. Officers shouldn't be guessing.
func TestFineIssue_UnknownDriverLicense_Rejects(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	body := fmt.Sprintf(`{
		"plate":%q,"offence_code":"INS_EXPIRED",
		"driver_license":"DL-DOES-NOT-EXIST",
		"geo_lat":60,"geo_lng":5,"device_id":"d","evidence":[
		{"kind":"photo","s3_key":"e","sha256":"abc","bytes":1,"taken_at":%q}]}`,
		plate, time.Now().UTC().Format(time.RFC3339))
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines", body, tok))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown_license") {
		t.Fatalf("want unknown_license code, got %s", rec.Body.String())
	}

	// And no fine row should have been created (the tx rolled back).
	var fineCount int
	if err := env.QueryRow(
		`SELECT count(*) FROM fines WHERE tenant_id=$1`, env.Tenant).
		Scan(&fineCount); err != nil {
		t.Fatal(err)
	}
	if fineCount != 0 {
		t.Fatalf("want 0 fines after 400, got %d", fineCount)
	}
}

// TestFineIssue_RecordsCustodyChain: when a fine is issued with
// evidence, every evidence item gets a 'captured' row in
// evidence_custody — the legal chain of custody starts here. The
// row records the officer's user_id, role, device_id, and the
// sha256 of the evidence so any later action (verified, viewed,
// exported) appends to a chain that proves who handled the file.
func TestFineIssue_RecordsCustodyChain(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, officerID := env.Token("officer", "fines:create", "fines:read")

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
		validIssueBody(plate, "INS_EXPIRED"), tok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	// One evidence item → one custody row, action='captured'.
	var rows int
	if err := env.QueryRow(
		`SELECT count(*) FROM evidence_custody
		   WHERE fine_id=$1 AND action='captured'`, out.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("want 1 custody row, got %d", rows)
	}

	// Actor metadata stamped from the JWT, not the body.
	var actor, role, device string
	if err := env.QueryRow(
		`SELECT actor_user::text, actor_role, actor_device FROM evidence_custody
		   WHERE fine_id=$1`, out.ID).Scan(&actor, &role, &device); err != nil {
		t.Fatal(err)
	}
	if actor != officerID {
		t.Fatalf("actor_user: want %s got %s", officerID, actor)
	}
	if role != "officer" {
		t.Fatalf("actor_role: want officer, got %s", role)
	}
	if device != "dev-test" {
		t.Fatalf("actor_device: want dev-test, got %s", device)
	}

	// And GET /v1/fines/{id} surfaces the custody history.
	rec = httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/fines/"+out.ID, "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"action":"captured"`) {
		t.Fatalf("response missing custody chain: %s", rec.Body.String())
	}
}

// TestFineIssue_CustodyRollsBackOnDuplicate: when the duplicate
// guard rejects a fine (409), the custody row from the failed
// attempt must NOT linger. The single custody row in event_outbox
// is the canonical proof.
func TestFineIssue_CustodyRollsBackOnDuplicate(t *testing.T) {
	env := testkit.Setup(t)
	_, plate := newVehicle(t, env)
	tok, _ := env.Token("officer", "fines:create")

	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		rec := httptest.NewRecorder()
		build(env).ServeHTTP(rec, env.Req("POST", "/v1/fines",
			validIssueBody(plate, "INS_EXPIRED"), tok))
		if rec.Code != want {
			t.Fatalf("attempt %d: want %d got %d %s", i, want, rec.Code, rec.Body.String())
		}
	}

	var rows int
	if err := env.QueryRow(
		`SELECT count(*) FROM evidence_custody WHERE tenant_id=$1`, env.Tenant).
		Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("want exactly 1 custody row (duplicate must roll back), got %d", rows)
	}
}
