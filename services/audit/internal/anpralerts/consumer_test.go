package anpralerts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/audit/internal/anpralerts"
)

// envForTest wraps the typed payload exactly the way the anpr-gateway
// publishes it.
func envForTest(tenant, vehicleID, plate string, stolen, seized, wanted bool) events.Envelope {
	return events.Envelope{
		ID: uuid.NewString(), Type: events.TypeAnprAlert, Version: 1,
		Source: "anpr-gateway", TenantID: tenant,
		OccurredAt: time.Now().UTC(),
		Data: events.AnprAlertPayload{
			ScanID: uuid.NewString(), Plate: plate, VehicleID: vehicleID,
			IsStolen: stolen, IsSeized: seized, IsWanted: wanted,
		},
	}
}

// TestHandle_WritesAlertWithSeverity: an anpr.alert with both stolen
// and wanted flags must land in audit_alerts with severity=2 and a
// details JSON that names both flags.
func TestHandle_WritesAlertWithSeverity(t *testing.T) {
	env := testkit.Setup(t)
	vid := uuid.New()
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate, is_stolen, is_wanted)
	          VALUES ($1, $2, 'AAA-111', true, true)`, vid, env.Tenant)

	if err := anpralerts.HandleForTest(context.Background(), env.AdminPool(),
		envForTest(env.Tenant, vid.String(), "AAA-111", true, false, true)); err != nil {
		t.Fatal(err)
	}

	var sev *float32
	var details string
	if err := env.QueryRow(
		`SELECT severity, details::text FROM audit_alerts
		  WHERE tenant_id=$1 AND kind='anpr_match_flagged_vehicle'
		    AND subject_id=$2`, env.Tenant, vid).
		Scan(&sev, &details); err != nil {
		t.Fatal(err)
	}
	if sev == nil || *sev != 2 {
		t.Fatalf("severity: want 2, got %v", sev)
	}
	if !contains(details, "stolen") || !contains(details, "wanted") {
		t.Fatalf("details missing flags: %s", details)
	}
}

// TestHandle_IdempotentSameDay: scanning the same flagged vehicle
// multiple times in one day must produce exactly one OPEN alert.
// The partial unique index over (tenant, kind, subject, day) WHERE
// resolved_at IS NULL handles this.
func TestHandle_IdempotentSameDay(t *testing.T) {
	env := testkit.Setup(t)
	vid := uuid.New()
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate, is_stolen)
	          VALUES ($1, $2, 'BBB-222', true)`, vid, env.Tenant)

	for i := 0; i < 5; i++ {
		if err := anpralerts.HandleForTest(context.Background(), env.AdminPool(),
			envForTest(env.Tenant, vid.String(), "BBB-222", true, false, false)); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := env.QueryRow(
		`SELECT count(*) FROM audit_alerts
		  WHERE tenant_id=$1 AND kind='anpr_match_flagged_vehicle'
		    AND subject_id=$2 AND resolved_at IS NULL`,
		env.Tenant, vid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 open alert after 5 scans, got %d", n)
	}
}

// TestHandle_NoFlagsSkipsAlert: the gateway should never publish an
// anpr.alert with all flags false, but if it did, the consumer must
// not write a meaningless audit_alerts row.
func TestHandle_NoFlagsSkipsAlert(t *testing.T) {
	env := testkit.Setup(t)
	vid := uuid.New()
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate)
	          VALUES ($1, $2, 'CCC-333')`, vid, env.Tenant)

	if err := anpralerts.HandleForTest(context.Background(), env.AdminPool(),
		envForTest(env.Tenant, vid.String(), "CCC-333", false, false, false)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := env.QueryRow(
		`SELECT count(*) FROM audit_alerts
		  WHERE tenant_id=$1 AND kind='anpr_match_flagged_vehicle'`,
		env.Tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("zero-flag alert should be skipped, got %d rows", n)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
