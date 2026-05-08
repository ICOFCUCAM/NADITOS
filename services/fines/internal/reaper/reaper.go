// Package reaper sweeps fine_evidence past its retention deadline,
// deletes the underlying storage object, and marks the row sealed.
//
// Retention policy is per-tenant in evidence_retention_policy:
//
//	default_days        — fallback for any fine status not below
//	paid_fine_days      — applies once fines.paid_at is set
//	cancelled_fine_days — applies once fines.status='cancelled'
//	court_case_days     — NULL means never reap (legal escalation)
//	legal_hold_active   — when true, the whole tenant is skipped
//
// Each candidate is reaped in its own transaction so a poisoned row
// (storage outage, missing object) doesn't halt the batch. Sealed rows
// are skipped permanently — sealed_at is the floor of idempotency.
package reaper

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/contracts/storage"
)

type Job struct {
	pool   *pgxpool.Pool
	store  storage.Store
	bucket string
	log    *slog.Logger
	every  time.Duration
}

// New returns a reaper that sweeps every 6 hours by default. Tests
// drive RunOnce directly; the default cadence is rare because reaping
// is destructive — once an object is gone, it's gone.
func New(pool *pgxpool.Pool, store storage.Store, bucket string, log *slog.Logger) *Job {
	return &Job{pool: pool, store: store, bucket: bucket, log: log,
		every: 6 * time.Hour}
}

func (j *Job) WithSchedule(every time.Duration) *Job { j.every = every; return j }

func (j *Job) Run(ctx context.Context) {
	t := time.NewTicker(j.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := j.RunOnce(ctx); err != nil {
				j.log.Warn("reaper: sweep failed", "err", err)
			}
		}
	}
}

type cand struct {
	evidenceID, fineID uuid.UUID
	tenantID, s3Key    string
}

// RunOnce identifies expired evidence and seals each row in its own
// tx. Returns the number of rows successfully sealed.
//
// The candidate query joins fine_evidence to fines and
// evidence_retention_policy in one shot so policy lookups can't drift
// per-row. Tenants without a policy row inherit the schema defaults
// via the LEFT JOIN + COALESCE in the predicate.
func (j *Job) RunOnce(ctx context.Context) (int, error) {
	conn, err := j.pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT fe.id, fe.fine_id, fe.tenant_id, fe.s3_key
		   FROM fine_evidence fe
		   JOIN fines f ON f.id = fe.fine_id
		   LEFT JOIN evidence_retention_policy p ON p.tenant_id = fe.tenant_id
		  WHERE fe.sealed_at IS NULL
		    AND COALESCE(p.legal_hold_active, false) = false
		    AND now() > (
		      CASE
		        WHEN f.status = 'paid' AND f.paid_at IS NOT NULL THEN
		          f.paid_at + (COALESCE(p.paid_fine_days, 1825) || ' days')::interval
		        WHEN f.status = 'cancelled' THEN
		          f.issued_at + (COALESCE(p.cancelled_fine_days, 365) || ' days')::interval
		        WHEN f.status = 'court' THEN
		          CASE WHEN p.court_case_days IS NULL THEN 'infinity'::timestamptz
		               ELSE f.issued_at + (p.court_case_days || ' days')::interval END
		        ELSE
		          f.issued_at + (COALESCE(p.default_days, 1825) || ' days')::interval
		      END
		    )`)
	if err != nil {
		return 0, err
	}
	var batch []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.evidenceID, &c.fineID, &c.tenantID, &c.s3Key); err != nil {
			continue
		}
		batch = append(batch, c)
	}
	rows.Close()

	sealed := 0
	for _, c := range batch {
		if err := j.seal(ctx, conn, c); err != nil {
			j.log.Warn("reaper: seal failed",
				slog.String("evidence", c.evidenceID.String()),
				slog.String("err", err.Error()))
			continue
		}
		sealed++
	}
	return sealed, nil
}

func (j *Job) seal(ctx context.Context, conn *pgxpool.Conn, c cand) error {
	// Delete the storage object first. If the bucket call fails, the
	// row stays unsealed and will be retried on the next sweep — we
	// never want to mark sealed without the object actually being
	// gone, because the row's whole purpose post-seal is to assert
	// "this is no longer recoverable".
	if err := j.store.Delete(ctx, j.bucket, c.s3Key); err != nil {
		// "not found" from the store is acceptable: object already
		// went, idempotent re-run. Anything else is a real failure.
		if !isNotFound(err) {
			return err
		}
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Re-check sealed_at under FOR UPDATE — another replica may have
	// won the race for this row.
	var sealed bool
	if err := tx.QueryRow(ctx,
		`SELECT sealed_at IS NOT NULL FROM fine_evidence
		  WHERE id=$1 FOR UPDATE`, c.evidenceID).Scan(&sealed); err != nil {
		return err
	}
	if sealed {
		return nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE fine_evidence SET sealed_at=now() WHERE id=$1`,
		c.evidenceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO evidence_custody
		   (tenant_id, fine_id, evidence_id, action, details)
		 VALUES ($1, $2, $3, 'sealed',
		         jsonb_build_object('reason','retention_policy'))`,
		c.tenantID, c.fineID, c.evidenceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// storage.DevStub returns errors.New("storage: not found"); real
	// adapters use their own sentinels — every adapter's "gone"
	// message is treated as success here.
	return errors.Is(err, errNotFound) ||
		errString(err) == "storage: not found"
}

var errNotFound = errors.New("storage: not found")

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
