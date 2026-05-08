package reaper_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/contracts/storage"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/fines/internal/reaper"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// recordingStore wraps DevStub and records every Delete call so tests
// can assert the reaper is the one issuing the bucket-side delete (and
// not, say, marking sealed without actually wiping the object).
type recordingStore struct {
	*storage.DevStub
	deleted []string
}

func (r *recordingStore) Delete(ctx context.Context, bucket, key string) error {
	r.deleted = append(r.deleted, bucket+"/"+key)
	return r.DevStub.Delete(ctx, bucket, key)
}

func newStore() *recordingStore {
	return &recordingStore{DevStub: storage.NewDevStub()}
}

// seedFineWithEvidence writes the minimum rows the reaper needs:
// a vehicle, a fine in the supplied status, an old fine_evidence
// row, and (optionally) an upload of the same key to the store.
func seedFineWithEvidence(t *testing.T, env *testkit.Env, store *recordingStore,
	status string, fineCreated time.Time, paidAt *time.Time) (fineID, evidenceID uuid.UUID, key string) {
	t.Helper()
	_, officer := env.Token("officer", "fines:create")
	vid := uuid.New()
	fineID = uuid.New()
	evidenceID = uuid.New()
	key = "evidence/" + uuid.NewString() + ".jpg"

	plate := "EVD-" + vid.String()[:6]
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate) VALUES ($1, $2, $3)`,
		vid, env.Tenant, plate)
	env.Exec(`INSERT INTO fines
	            (id, tenant_id, vehicle_id, plate, offence_code, amount, currency,
	             status, issued_at, due_at, paid_at, issued_by)
	          VALUES ($1, $2, $3, $4, 'INS_EXPIRED', 100, 'EUR', $5::fine_status,
	                  $6::timestamptz, $6::timestamptz + interval '14 days', $7,
	                  $8::uuid)`,
		fineID, env.Tenant, vid, plate, status, fineCreated, paidAt, officer)
	env.Exec(`INSERT INTO fine_evidence
	            (id, tenant_id, fine_id, kind, s3_key, sha256, bytes, taken_at, created_at)
	          VALUES ($1, $2, $3, 'photo', $4, 'deadbeef', 1234, $5, $5)`,
		evidenceID, env.Tenant, fineID, key, fineCreated)
	if _, err := store.Put(context.Background(), "evidence", key,
		"image/jpeg", staticReader{}); err != nil {
		t.Fatal(err)
	}
	return
}

type staticReader struct{}

func (staticReader) Read(p []byte) (int, error) { return 0, io.EOF }

// TestReaper_SealsExpiredPaidFine: a paid fine older than the
// per-tenant paid_fine_days policy must have its evidence sealed,
// the storage object deleted, and a custody row written. The fine
// itself stays — only the photo blob and its s3_key go.
func TestReaper_SealsExpiredPaidFine(t *testing.T) {
	env := testkit.Setup(t)
	store := newStore()

	// 1-day policy so we can backdate the fine and trigger the sweep.
	env.Exec(`INSERT INTO evidence_retention_policy
	            (tenant_id, default_days, paid_fine_days, cancelled_fine_days)
	          VALUES ($1, 1, 1, 1)
	          ON CONFLICT (tenant_id) DO UPDATE
	          SET default_days=1, paid_fine_days=1, cancelled_fine_days=1`,
		env.Tenant)
	paid := time.Now().Add(-3 * 24 * time.Hour)
	_, evidenceID, key := seedFineWithEvidence(t, env, store, "paid",
		time.Now().Add(-7*24*time.Hour), &paid)

	r := reaper.New(env.AdminPool(), store, "evidence", discardLogger())
	n, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 sealed, got %d", n)
	}
	var sealed bool
	if err := env.QueryRow(
		`SELECT sealed_at IS NOT NULL FROM fine_evidence WHERE id=$1`,
		evidenceID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if !sealed {
		t.Fatal("row not marked sealed")
	}
	if len(store.deleted) != 1 || store.deleted[0] != "evidence/"+key {
		t.Fatalf("delete not called: %v", store.deleted)
	}
	var custody int
	if err := env.QueryRow(
		`SELECT count(*) FROM evidence_custody
		  WHERE evidence_id=$1 AND action='sealed'`,
		evidenceID).Scan(&custody); err != nil {
		t.Fatal(err)
	}
	if custody != 1 {
		t.Fatalf("want 1 sealed custody row, got %d", custody)
	}
}

// TestReaper_SkipsLegalHold: when legal_hold_active is true on the
// tenant policy, even fines past their retention deadline must NOT
// be reaped — the citizen has filed a court action and the photo
// has to survive until counsel clears it.
func TestReaper_SkipsLegalHold(t *testing.T) {
	env := testkit.Setup(t)
	store := newStore()

	env.Exec(`INSERT INTO evidence_retention_policy
	            (tenant_id, default_days, paid_fine_days, cancelled_fine_days, legal_hold_active)
	          VALUES ($1, 1, 1, 1, true)
	          ON CONFLICT (tenant_id) DO UPDATE
	          SET legal_hold_active=true, default_days=1, paid_fine_days=1`,
		env.Tenant)
	paid := time.Now().Add(-30 * 24 * time.Hour)
	_, evidenceID, _ := seedFineWithEvidence(t, env, store, "paid",
		time.Now().Add(-60*24*time.Hour), &paid)

	r := reaper.New(env.AdminPool(), store, "evidence", discardLogger())
	n, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 reaped under legal hold, got %d", n)
	}
	var sealed bool
	if err := env.QueryRow(
		`SELECT sealed_at IS NOT NULL FROM fine_evidence WHERE id=$1`,
		evidenceID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if sealed {
		t.Fatal("evidence sealed despite legal hold")
	}
}

// TestReaper_Idempotent: a second RunOnce against the same data
// must not seal the row again, must not call store.Delete a second
// time, and must not append another custody row.
func TestReaper_Idempotent(t *testing.T) {
	env := testkit.Setup(t)
	store := newStore()

	env.Exec(`INSERT INTO evidence_retention_policy
	            (tenant_id, default_days, paid_fine_days, cancelled_fine_days)
	          VALUES ($1, 1, 1, 1)
	          ON CONFLICT (tenant_id) DO UPDATE
	          SET default_days=1, paid_fine_days=1`,
		env.Tenant)
	paid := time.Now().Add(-3 * 24 * time.Hour)
	_, evidenceID, _ := seedFineWithEvidence(t, env, store, "paid",
		time.Now().Add(-7*24*time.Hour), &paid)

	r := reaper.New(env.AdminPool(), store, "evidence", discardLogger())
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	deletesAfterFirst := len(store.deleted)
	n, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second run sealed %d rows; want 0", n)
	}
	if len(store.deleted) != deletesAfterFirst {
		t.Fatalf("second run hit storage: %d → %d", deletesAfterFirst, len(store.deleted))
	}
	var custody int
	if err := env.QueryRow(
		`SELECT count(*) FROM evidence_custody
		  WHERE evidence_id=$1 AND action='sealed'`,
		evidenceID).Scan(&custody); err != nil {
		t.Fatal(err)
	}
	if custody != 1 {
		t.Fatalf("want exactly 1 sealed custody row, got %d", custody)
	}
}
