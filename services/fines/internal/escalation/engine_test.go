package escalation_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/fines/internal/escalation"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// seedFine inserts a fine with a specific due_at and current
// escalation_stage so we can drive the engine through stages.
func seedFine(t *testing.T, env *testkit.Env, dueAt time.Time, status string, stage int) string {
	t.Helper()
	id := uuid.NewString()
	uid := uuid.New()
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	         VALUES ($1, $2, $3, '!', 'Officer')`,
		uid, env.Tenant, "off-"+uid.String()[:8]+"@x")
	env.Exec(`INSERT INTO fines
	         (id, tenant_id, plate, offence_code, amount, currency,
	          status, issued_by, issued_at, due_at, escalation_stage)
	         VALUES ($1, $2, 'ESC-1', 'INS_EXPIRED', 400.00, 'EUR',
	                 $3::fine_status, $4, now(), $5, $6)`,
		id, env.Tenant, status, uid, dueAt, stage)
	return id
}

// seedEscalationStages applies a default 1d/3d/7d/14d/21d ladder so
// tests can simulate aging by setting due_at in the past.
func seedEscalationStages(t *testing.T, env *testkit.Env) {
	t.Helper()
	for _, row := range []struct {
		stage     int
		afterDays int
		action    string
	}{
		{1, 1, "warning"},
		{2, 3, "penalty"},
		{3, 7, "flag"},
		{4, 14, "seize"},
		{5, 21, "court"},
	} {
		env.Exec(`INSERT INTO regulation_escalation
		         (tenant_id, stage, after_days, multiplier, action)
		         VALUES ($1, $2, $3, 1.0, $4)
		         ON CONFLICT (tenant_id, stage) DO UPDATE
		           SET after_days=EXCLUDED.after_days, action=EXCLUDED.action`,
			env.Tenant, row.stage, row.afterDays, row.action)
	}
}

// TestEscalation_AdvancesPastDueFines: a fine 2 days past due moves
// from stage 0 to stage 1 (warning).
func TestEscalation_AdvancesPastDueFines(t *testing.T) {
	env := testkit.Setup(t)
	seedEscalationStages(t, env)
	fid := seedFine(t, env, time.Now().Add(-2*24*time.Hour), "issued", 0)

	if err := escalation.New(env.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var stage int
	var status string
	if err := env.QueryRow(
		`SELECT escalation_stage, status::text FROM fines WHERE id=$1`, fid).
		Scan(&stage, &status); err != nil {
		t.Fatal(err)
	}
	if stage != 1 {
		t.Fatalf("want stage=1, got %d", stage)
	}
	if status != "warned" {
		t.Fatalf("want status=warned, got %s", status)
	}
}

// TestEscalation_JumpsToHighestApplicableStage: a fine 30 days past
// due jumps directly to stage 5 (court) — we don't iterate stage by
// stage on each sweep.
func TestEscalation_JumpsToHighestApplicableStage(t *testing.T) {
	env := testkit.Setup(t)
	seedEscalationStages(t, env)
	fid := seedFine(t, env, time.Now().Add(-30*24*time.Hour), "issued", 0)

	if err := escalation.New(env.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var stage int
	var status string
	_ = env.QueryRow(
		`SELECT escalation_stage, status::text FROM fines WHERE id=$1`, fid).
		Scan(&stage, &status)
	if stage != 5 {
		t.Fatalf("want stage=5, got %d", stage)
	}
	if status != "court" {
		t.Fatalf("want status=court, got %s", status)
	}
}

// TestEscalation_SkipsTerminalFines: fines that are paid, cancelled,
// or disputed are not escalated even if past due.
func TestEscalation_SkipsTerminalFines(t *testing.T) {
	env := testkit.Setup(t)
	seedEscalationStages(t, env)
	for _, status := range []string{"paid", "cancelled", "disputed"} {
		fid := seedFine(t, env, time.Now().Add(-30*24*time.Hour), status, 0)
		if err := escalation.New(env.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		var stage int
		_ = env.QueryRow(
			`SELECT escalation_stage FROM fines WHERE id=$1`, fid).Scan(&stage)
		if stage != 0 {
			t.Fatalf("status=%s: want stage=0 unchanged, got %d", status, stage)
		}
	}
}

// TestEscalation_Idempotent: running twice produces the same stage.
// A subsequent RunOnce after the first must be a no-op.
func TestEscalation_Idempotent(t *testing.T) {
	env := testkit.Setup(t)
	seedEscalationStages(t, env)
	fid := seedFine(t, env, time.Now().Add(-2*24*time.Hour), "issued", 0)

	job := escalation.New(env.AdminPool(), discardLogger())
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := env.QueryRow(
		`SELECT count(*) FROM event_outbox
		  WHERE tenant_id=$1
		    AND envelope->>'type'='fine.escalated'
		    AND envelope->'data'->>'fine_id'=$2`,
		env.Tenant, fid).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("want 1 fine.escalated outbox row, got %d", rows)
	}
}

// TestEscalation_EmitsOutboxEvent: every advance writes a
// fine.escalated row in event_outbox, so the notifications consumer
// can pick it up.
func TestEscalation_EmitsOutboxEvent(t *testing.T) {
	env := testkit.Setup(t)
	seedEscalationStages(t, env)
	fid := seedFine(t, env, time.Now().Add(-2*24*time.Hour), "issued", 0)
	if err := escalation.New(env.AdminPool(), discardLogger()).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	var fromStage, toStage int
	var action, newStatus string
	if err := env.QueryRow(
		`SELECT (envelope->'data'->>'from_stage')::int,
		        (envelope->'data'->>'to_stage')::int,
		        envelope->'data'->>'action',
		        envelope->'data'->>'new_status'
		   FROM event_outbox
		  WHERE tenant_id=$1
		    AND envelope->>'type'='fine.escalated'
		    AND envelope->'data'->>'fine_id'=$2`,
		env.Tenant, fid).Scan(&fromStage, &toStage, &action, &newStatus); err != nil {
		t.Fatal(err)
	}
	if fromStage != 0 || toStage != 1 || action != "warning" || newStatus != "warned" {
		t.Fatalf("envelope mismatch: from=%d to=%d action=%s status=%s",
			fromStage, toStage, action, newStatus)
	}
}
