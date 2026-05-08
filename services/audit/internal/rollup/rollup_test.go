package rollup_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/audit/internal/rollup"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// seedOfficer creates a user we can stamp as fines.issued_by.
func seedOfficer(t *testing.T, env *testkit.Env) uuid.UUID {
	t.Helper()
	id := uuid.New()
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	         VALUES ($1, $2, $3, '!', 'Officer')`,
		id, env.Tenant, fmt.Sprintf("%s@%s", id.String()[:8], env.Tenant))
	return id
}

// seedFine inserts a fine on a specific UTC date for a given officer.
func seedFine(t *testing.T, env *testkit.Env, officer uuid.UUID, date string, status string, amount string) {
	t.Helper()
	env.Exec(`INSERT INTO fines
	         (tenant_id, plate, offence_code, amount, currency, status,
	          issued_by, issued_at, due_at)
	         VALUES ($1, $2, 'INS_EXPIRED', $3::numeric, 'EUR', $4::fine_status,
	                 $5, ($6::date + interval '12 hours'),
	                 ($6::date + interval '14 days'))`,
		env.Tenant, "PLATE-"+uuid.NewString()[:6], amount, status, officer, date)
}

// TestAggregate_PerOfficerPerDayCounts: insert 5 fines for officer A and
// 1 for officer B on the same day; aggregate; expect two stat rows
// with the right counts and totals.
func TestAggregate_PerOfficerPerDayCounts(t *testing.T) {
	env := testkit.Setup(t)
	a := seedOfficer(t, env)
	b := seedOfficer(t, env)

	day := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < 5; i++ {
		seedFine(t, env, a, day, "issued", "100.00")
	}
	seedFine(t, env, b, day, "issued", "50.00")

	job := rollup.New(env.AdminPool(), discardLogger())
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Officer A: 5 fines, 500.00.
	var issuedA int
	var totalA string
	if err := env.QueryRow(
		`SELECT fines_issued, fines_total::text
		   FROM officer_daily_stats
		  WHERE tenant_id=$1 AND officer_id=$2 AND day=$3::date`,
		env.Tenant, a, day).Scan(&issuedA, &totalA); err != nil {
		t.Fatal(err)
	}
	if issuedA != 5 || totalA != "500.00" {
		t.Fatalf("officer A: want 5 / 500.00, got %d / %s", issuedA, totalA)
	}

	// Officer B: 1 fine, 50.00.
	var issuedB int
	if err := env.QueryRow(
		`SELECT fines_issued FROM officer_daily_stats
		  WHERE tenant_id=$1 AND officer_id=$2 AND day=$3::date`,
		env.Tenant, b, day).Scan(&issuedB); err != nil {
		t.Fatal(err)
	}
	if issuedB != 1 {
		t.Fatalf("officer B: want 1, got %d", issuedB)
	}
}

// TestAggregate_CancelledExcludedFromTotal: cancelled fines should
// count in fines_cancelled but NOT in fines_total — the dashboard sums
// money actually owed, not money that an admin already wrote off.
func TestAggregate_CancelledExcludedFromTotal(t *testing.T) {
	env := testkit.Setup(t)
	o := seedOfficer(t, env)
	day := time.Now().UTC().Format("2006-01-02")
	seedFine(t, env, o, day, "issued", "100.00")
	seedFine(t, env, o, day, "cancelled", "100.00")

	if err := rollup.New(env.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var issued, cancelled int
	var total string
	if err := env.QueryRow(
		`SELECT fines_issued, fines_cancelled, fines_total::text
		   FROM officer_daily_stats
		  WHERE tenant_id=$1 AND officer_id=$2`,
		env.Tenant, o).Scan(&issued, &cancelled, &total); err != nil {
		t.Fatal(err)
	}
	if issued != 2 {
		t.Fatalf("issued: want 2, got %d", issued)
	}
	if cancelled != 1 {
		t.Fatalf("cancelled: want 1, got %d", cancelled)
	}
	if total != "100.00" {
		t.Fatalf("total: want 100.00 (cancelled excluded), got %s", total)
	}
}

// TestScore_ZeroVarianceBaseline_IsNull: when the baseline has zero
// variance (5 days of "1 fine each"), the z-score formula divides by
// zero — the scorer must return NULL rather than NaN/Inf so the
// dashboard can simply skip those rows.
func TestScore_ZeroVarianceBaseline_IsNull(t *testing.T) {
	env := testkit.Setup(t)
	o := seedOfficer(t, env)
	end := time.Now().UTC()
	dates := []string{
		end.AddDate(0, 0, -5).Format("2006-01-02"),
		end.AddDate(0, 0, -4).Format("2006-01-02"),
		end.AddDate(0, 0, -3).Format("2006-01-02"),
		end.AddDate(0, 0, -2).Format("2006-01-02"),
		end.AddDate(0, 0, -1).Format("2006-01-02"),
		end.Format("2006-01-02"),
	}
	for _, d := range dates[:5] {
		seedFine(t, env, o, d, "issued", "10.00")
	}
	for i := 0; i < 20; i++ {
		seedFine(t, env, o, dates[5], "issued", "10.00")
	}
	if err := rollup.New(env.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var score *float32
	if err := env.QueryRow(
		`SELECT anomaly_score FROM officer_daily_stats
		  WHERE tenant_id=$1 AND officer_id=$2 AND day=$3::date`,
		env.Tenant, o, dates[5]).Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score != nil {
		t.Fatalf("zero-variance baseline must yield NULL score, got %f", *score)
	}
}

// TestScore_RealisticBaseline: prior days have variance, so the score
// is a meaningful number. Day 6's 20-fine spike must produce a large
// positive z-score.
func TestScore_RealisticBaseline(t *testing.T) {
	env := testkit.Setup(t)
	o := seedOfficer(t, env)

	end := time.Now().UTC()
	dates := []string{
		end.AddDate(0, 0, -7).Format("2006-01-02"),
		end.AddDate(0, 0, -6).Format("2006-01-02"),
		end.AddDate(0, 0, -5).Format("2006-01-02"),
		end.AddDate(0, 0, -4).Format("2006-01-02"),
		end.AddDate(0, 0, -3).Format("2006-01-02"),
		end.AddDate(0, 0, -2).Format("2006-01-02"),
		end.AddDate(0, 0, -1).Format("2006-01-02"),
		end.Format("2006-01-02"),
	}
	// Baseline with variance: 2, 3, 1, 4, 2, 3, 1
	for i, n := range []int{2, 3, 1, 4, 2, 3, 1} {
		for k := 0; k < n; k++ {
			seedFine(t, env, o, dates[i], "issued", "10.00")
		}
	}
	// Spike: 20 on the last day.
	for i := 0; i < 20; i++ {
		seedFine(t, env, o, dates[7], "issued", "10.00")
	}

	if err := rollup.New(env.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var score *float32
	if err := env.QueryRow(
		`SELECT anomaly_score FROM officer_daily_stats
		  WHERE tenant_id=$1 AND officer_id=$2 AND day=$3::date`,
		env.Tenant, o, dates[7]).Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score == nil {
		t.Fatal("z-score should be computed; got NULL")
	}
	if *score < 5.0 {
		t.Fatalf("spike day z-score should be >5, got %f", *score)
	}

	// The first day has no baseline → NULL.
	var firstScore *float32
	if err := env.QueryRow(
		`SELECT anomaly_score FROM officer_daily_stats
		  WHERE tenant_id=$1 AND officer_id=$2 AND day=$3::date`,
		env.Tenant, o, dates[0]).Scan(&firstScore); err != nil {
		t.Fatal(err)
	}
	if firstScore != nil {
		t.Fatalf("first day should be NULL (no baseline), got %f", *firstScore)
	}
}

// TestRollup_Idempotent: running twice produces the same numbers, no
// duplicates. Critical because the scheduler and the on-demand admin
// trigger may overlap.
func TestRollup_Idempotent(t *testing.T) {
	env := testkit.Setup(t)
	o := seedOfficer(t, env)
	day := time.Now().UTC().Format("2006-01-02")
	for i := 0; i < 3; i++ {
		seedFine(t, env, o, day, "issued", "10.00")
	}
	job := rollup.New(env.AdminPool(), discardLogger())
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := env.QueryRow(
		`SELECT count(*) FROM officer_daily_stats
		  WHERE tenant_id=$1 AND officer_id=$2 AND day=$3::date`,
		env.Tenant, o, day).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("want exactly 1 stat row after 2 runs, got %d", rows)
	}
}

// TestRollup_TenantIsolation: tenant A's officer rows don't appear in
// tenant B's stats. RLS isn't strictly required here (the rollup runs
// with BYPASSRLS) but the per-tenant filter in the WHERE clause is —
// proves the aggregator scopes correctly.
func TestRollup_TenantIsolation(t *testing.T) {
	envA := testkit.Setup(t)
	envB := testkit.Setup(t)
	a := seedOfficer(t, envA)
	day := time.Now().UTC().Format("2006-01-02")
	seedFine(t, envA, a, day, "issued", "10.00")

	if err := rollup.New(envA.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var seen int
	if err := envB.QueryRow(
		`SELECT count(*) FROM officer_daily_stats WHERE tenant_id=$1`,
		envB.Tenant).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Fatalf("tenant B saw %d rows from A's rollup, expected 0", seen)
	}
}
