package demerit_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/license/internal/demerit"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// captureLogger writes everything from Debug up into a buffer the test
// can dump on failure. Avoids the discard-logger silencing engine errors.
func captureLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	t.Cleanup(func() {
		if t.Failed() && buf.Len() > 0 {
			t.Logf("engine logs:\n%s", buf.String())
		}
	})
	return slog.New(h), buf
}

// seedLicense creates a driver license for a tenant and returns its id.
// The country pack already seeded by testkit attaches points to offences.
func seedLicense(t *testing.T, env *testkit.Env, points int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	env.Exec(`INSERT INTO driver_licenses
	         (id, tenant_id, license_number, full_name, classes,
	          issued_at, expires_at, points)
	         VALUES ($1,$2,$3,$4,$5,$6::date,$7::date,$8)`,
		id, env.Tenant, "DL-"+id.String()[:6], "Test Driver",
		[]string{"B"}, "2020-01-01", "2030-01-01", points)
	return id
}

// seedFine inserts a fine row directly (bypassing the fines service) and
// returns its id. The handler can't insert via the app pool because the
// vehicle/owner FKs aren't fully set up for a license-only test.
func seedFine(t *testing.T, env *testkit.Env, licenseID uuid.UUID, offenceCode string, officerID uuid.UUID) uuid.UUID {
	t.Helper()
	fid := uuid.New()
	env.Exec(`INSERT INTO fines
	         (id, tenant_id, plate, offence_code, amount, currency,
	          driver_license_id, issued_by, due_at)
	         VALUES ($1,$2,'TEST-PLATE',$3, 100.00, 'EUR', $4, $5, now() + interval '14 days')`,
		fid, env.Tenant, offenceCode, licenseID, officerID)
	return fid
}

// TestDemerit_PointsAppliedFromCountryPack: when fine.issued fires, the
// engine reads points from the country_pack manifest and updates the
// license + ledger.
func TestDemerit_PointsAppliedFromCountryPack(t *testing.T) {
	env := testkit.Setup(t)
	log, _ := captureLogger(t)
	bus := events.NewInProc(log)
	auditCl := audit.New("", "license")
	eng := demerit.New(env.AdminPool(), log, auditCl, bus)
	eng.Wire(bus)

	_, officerID := env.Token("officer")
	officer, _ := uuid.Parse(officerID)
	licID := seedLicense(t, env, 0)
	fid := seedFine(t, env, licID, "INS_EXPIRED", officer)

	// Publish the event the demerit engine subscribes to.
	env_ := events.NewEnvelope("fines", env.Tenant, events.TypeFineIssued, 1,
		events.FineIssuedPayload{FineID: fid.String(), OffenceCode: "INS_EXPIRED"})
	if err := bus.Publish(context.Background(), env_); err != nil {
		t.Fatal(err)
	}

	var points int
	if err := env.QueryRow(`SELECT points FROM driver_licenses WHERE id=$1`, licID).Scan(&points); err != nil {
		t.Fatal(err)
	}
	if points != 4 {
		t.Fatalf("INS_EXPIRED is 4 points in the test pack; got %d", points)
	}

	// Ledger should contain a +4 row.
	var delta int
	if err := env.QueryRow(`SELECT delta FROM driver_demerit_events WHERE license_id=$1
	                          ORDER BY id DESC LIMIT 1`, licID).Scan(&delta); err != nil {
		t.Fatal(err)
	}
	if delta != 4 {
		t.Fatalf("ledger delta want 4, got %d", delta)
	}
}

// TestDemerit_ThresholdSuspendsLicense: accumulating points past the
// configured threshold opens an active suspension and flips the license.
func TestDemerit_ThresholdSuspendsLicense(t *testing.T) {
	env := testkit.Setup(t)
	bus := events.NewInProc(discardLogger())
	auditCl := audit.New("", "license")
	eng := demerit.New(env.AdminPool(), discardLogger(), auditCl, bus)
	eng.Wire(bus)

	_, officerID := env.Token("officer")
	officer, _ := uuid.Parse(officerID)
	licID := seedLicense(t, env, 0)

	// 12-point threshold; SPEED_30 = 6 → two events crosses it.
	for i := 0; i < 2; i++ {
		fid := seedFine(t, env, licID, "SPEED_30", officer)
		_ = bus.Publish(context.Background(),
			events.NewEnvelope("fines", env.Tenant, events.TypeFineIssued, 1,
				events.FineIssuedPayload{FineID: fid.String(), OffenceCode: "SPEED_30"}))
	}

	var (
		points       int
		isSuspended  bool
	)
	if err := env.QueryRow(
		`SELECT points, is_suspended FROM driver_licenses WHERE id=$1`,
		licID).Scan(&points, &isSuspended); err != nil {
		t.Fatal(err)
	}
	if points != 12 {
		t.Fatalf("want 12 points after 2x SPEED_30, got %d", points)
	}
	if !isSuspended {
		t.Fatal("license should be suspended at threshold")
	}

	var suspensions int
	if err := env.QueryRow(
		`SELECT count(*) FROM driver_suspensions
		   WHERE license_id=$1 AND trigger_kind='demerit' AND lifted_at IS NULL`,
		licID).Scan(&suspensions); err != nil {
		t.Fatal(err)
	}
	if suspensions != 1 {
		t.Fatalf("want exactly 1 active demerit suspension, got %d", suspensions)
	}

	// Idempotency: a third over-threshold fine should NOT open a second
	// suspension (the engine checks for an existing active one).
	fid := seedFine(t, env, licID, "SPEED_30", officer)
	_ = bus.Publish(context.Background(),
		events.NewEnvelope("fines", env.Tenant, events.TypeFineIssued, 1,
			events.FineIssuedPayload{FineID: fid.String(), OffenceCode: "SPEED_30"}))
	if err := env.QueryRow(
		`SELECT count(*) FROM driver_suspensions
		   WHERE license_id=$1 AND trigger_kind='demerit' AND lifted_at IS NULL`,
		licID).Scan(&suspensions); err != nil {
		t.Fatal(err)
	}
	if suspensions != 1 {
		t.Fatalf("expected suspension to remain singleton, got %d", suspensions)
	}
	_ = time.Second // keep import live in case the file shrinks
}

// TestDemerit_NoLicenseLinkSkips: fines without driver_license_id are
// no-ops for the demerit engine — no ledger row, no suspension.
func TestDemerit_NoLicenseLinkSkips(t *testing.T) {
	env := testkit.Setup(t)
	bus := events.NewInProc(discardLogger())
	auditCl := audit.New("", "license")
	eng := demerit.New(env.AdminPool(), discardLogger(), auditCl, bus)
	eng.Wire(bus)

	_, officerID := env.Token("officer")
	officer, _ := uuid.Parse(officerID)
	// Fine without a driver license link.
	fid := uuid.New()
	env.Exec(`INSERT INTO fines
	         (id, tenant_id, plate, offence_code, amount, currency, issued_by, due_at)
	         VALUES ($1,$2,'NO-DRIVER','SPEED_30', 500.00, 'EUR', $3, now() + interval '14 days')`,
		fid, env.Tenant, officer)
	_ = bus.Publish(context.Background(),
		events.NewEnvelope("fines", env.Tenant, events.TypeFineIssued, 1,
			events.FineIssuedPayload{FineID: fid.String(), OffenceCode: "SPEED_30"}))

	var ledgerCount int
	if err := env.QueryRow(`SELECT count(*) FROM driver_demerit_events WHERE tenant_id=$1`, env.Tenant).
		Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 0 {
		t.Fatalf("expected no demerit rows for licenseless fine, got %d", ledgerCount)
	}
}

// TestDemerit_WritesToOutbox: a demerit application puts the
// license.demerit envelope in event_outbox in the same tx as the
// ledger update — that's what makes the notifications consumer (and
// every future analytics consumer) able to react.
func TestDemerit_WritesToOutbox(t *testing.T) {
	env := testkit.Setup(t)
	bus := events.NewInProc(discardLogger())
	auditCl := audit.New("", "license")
	eng := demerit.New(env.AdminPool(), discardLogger(), auditCl, bus)
	eng.Wire(bus)

	_, officerID := env.Token("officer")
	officer, _ := uuid.Parse(officerID)
	licID := seedLicense(t, env, 0)
	fid := seedFine(t, env, licID, "INS_EXPIRED", officer)
	_ = bus.Publish(context.Background(),
		events.NewEnvelope("fines", env.Tenant, events.TypeFineIssued, 1,
			events.FineIssuedPayload{FineID: fid.String(), OffenceCode: "INS_EXPIRED"}))

	var demeritEvents int
	if err := env.QueryRow(
		`SELECT count(*) FROM event_outbox
		   WHERE tenant_id=$1
		     AND envelope->>'type'='license.demerit'
		     AND envelope->'data'->>'license_id'=$2`,
		env.Tenant, licID.String()).Scan(&demeritEvents); err != nil {
		t.Fatal(err)
	}
	if demeritEvents != 1 {
		t.Fatalf("expected exactly 1 license.demerit outbox row, got %d", demeritEvents)
	}
}

// TestDemerit_SuspendedAlsoOutboxed: when threshold is crossed in a
// single fine event, both license.demerit AND license.suspended must
// land in event_outbox so the citizen gets the suspension notice
// without us double-booking direct bus.Publish calls.
func TestDemerit_SuspendedAlsoOutboxed(t *testing.T) {
	env := testkit.Setup(t)
	bus := events.NewInProc(discardLogger())
	auditCl := audit.New("", "license")
	eng := demerit.New(env.AdminPool(), discardLogger(), auditCl, bus)
	eng.Wire(bus)

	_, officerID := env.Token("officer")
	officer, _ := uuid.Parse(officerID)
	licID := seedLicense(t, env, 0)

	// Two SPEED_30 (6 points each) crosses the 12-point threshold on
	// the second fine; that publish must include license.suspended.
	for i := 0; i < 2; i++ {
		fid := seedFine(t, env, licID, "SPEED_30", officer)
		_ = bus.Publish(context.Background(),
			events.NewEnvelope("fines", env.Tenant, events.TypeFineIssued, 1,
				events.FineIssuedPayload{FineID: fid.String(), OffenceCode: "SPEED_30"}))
	}

	// Two demerit envelopes; one suspension envelope (the second
	// trigger is suppressed by the existing-suspension guard).
	var demerit, suspended int
	if err := env.QueryRow(
		`SELECT count(*) FROM event_outbox
		   WHERE tenant_id=$1 AND envelope->>'type'='license.demerit'
		     AND envelope->'data'->>'license_id'=$2`,
		env.Tenant, licID.String()).Scan(&demerit); err != nil {
		t.Fatal(err)
	}
	if demerit != 2 {
		t.Fatalf("want 2 license.demerit, got %d", demerit)
	}
	if err := env.QueryRow(
		`SELECT count(*) FROM event_outbox
		   WHERE tenant_id=$1 AND envelope->>'type'='license.suspended'
		     AND envelope->'data'->>'license_id'=$2`,
		env.Tenant, licID.String()).Scan(&suspended); err != nil {
		t.Fatal(err)
	}
	if suspended != 1 {
		t.Fatalf("want 1 license.suspended, got %d", suspended)
	}
}
