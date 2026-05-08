// Package escalation walks unpaid fines through their per-tenant
// escalation stages.
//
// regulation_escalation is keyed by (tenant_id, stage). Each row says
// "at +after_days past due_at, the fine moves to stage N with action
// X and an effective amount × multiplier."
//
// Default stages from the demo pack:
//   1: +7d   warning
//   2: +14d  penalty
//   3: +30d  flag
//   4: +60d  seize
//   5: +90d  court
//
// The engine's loop:
//   - find every fine whose status is in (issued, warned, overdue, escalated)
//     and whose escalation_stage < max_stage_for_tenant
//   - for each, find the highest stage whose after_days <= now() - due_at
//   - if the fine's escalation_stage is below that target, advance:
//       UPDATE fines SET escalation_stage=target, status=action_to_status(action)
//       outbox a fine.escalated event
//   - notification consumer renders a citizen-facing message per stage.
//
// Idempotency is structural: only fines BELOW the target are advanced.
// Repeated runs are safe; replicas use SKIP LOCKED on the candidate row.
package escalation

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/events"
)

type Job struct {
	pool  *pgxpool.Pool
	log   *slog.Logger
	every time.Duration
}

// New returns an escalation Job. Default sweep is every 5 minutes.
// Test helpers can override via WithSchedule for faster runs.
func New(pool *pgxpool.Pool, log *slog.Logger) *Job {
	return &Job{pool: pool, log: log, every: 5 * time.Minute}
}

func (j *Job) WithSchedule(every time.Duration) *Job { j.every = every; return j }

func (j *Job) Run(ctx context.Context) {
	if err := j.RunOnce(ctx); err != nil {
		j.log.Warn("escalation: initial sweep failed", "err", err)
	}
	t := time.NewTicker(j.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := j.RunOnce(ctx); err != nil {
				j.log.Warn("escalation: sweep failed", "err", err)
			}
		}
	}
}

// RunOnce performs one sweep. Returns nil if there's nothing to do.
// Each candidate fine is advanced in its OWN transaction so a poisoned
// row (e.g. a deleted tenant) doesn't halt the rest of the batch.
func (j *Job) RunOnce(ctx context.Context) error {
	conn, err := j.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Find candidate fines and their target stage in one query.
	// FOR UPDATE SKIP LOCKED so multiple replicas can sweep concurrently.
	rows, err := conn.Query(ctx,
		`SELECT f.id, f.tenant_id, f.escalation_stage, target.stage, target.action
		   FROM fines f
		   JOIN LATERAL (
		     SELECT esc.stage, esc.action
		       FROM regulation_escalation esc
		      WHERE esc.tenant_id = f.tenant_id
		        AND esc.after_days <= EXTRACT(EPOCH FROM (now() - f.due_at)) / 86400
		      ORDER BY esc.stage DESC
		      LIMIT 1
		   ) target ON true
		  WHERE f.status NOT IN ('paid','cancelled','disputed')
		    AND f.escalation_stage < target.stage`)
	if err != nil {
		return err
	}
	type cand struct {
		fineID, tenantID string
		curStage         int
		nextStage        int
		action           string
	}
	var batch []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.fineID, &c.tenantID, &c.curStage, &c.nextStage, &c.action); err != nil {
			continue
		}
		batch = append(batch, c)
	}
	rows.Close()
	if len(batch) == 0 {
		return nil
	}

	for _, c := range batch {
		if err := j.advance(ctx, conn, c.fineID, c.tenantID, c.curStage, c.nextStage, c.action); err != nil {
			j.log.Warn("escalation: advance failed",
				slog.String("fine", c.fineID),
				slog.Int("from", c.curStage),
				slog.Int("to", c.nextStage),
				slog.String("err", err.Error()))
			// Continue with the next candidate — one failed row must
			// not halt the sweep.
		}
	}
	return nil
}

// advance bumps the fine's escalation_stage and stamps the right
// status, then writes a fine.escalated outbox event in the same tx.
//
// Stage → status mapping. A stage's `action` describes the operational
// step the platform takes; status carries the legal designation:
//
//   warning  → status stays as-is (citizen reminded)
//   penalty  → status='warned' (multiplier applied earlier; we don't
//              re-charge — the citizen still owes the original amount)
//   flag     → status='overdue' (visible in police lookups as red)
//   seize    → status='seized' (vehicle on enforcement watch list)
//   court    → status='court' (out of administrative remedy)
func (j *Job) advance(ctx context.Context, conn pgxConn,
	fineID, tenantID string, fromStage, toStage int, action string) error {

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Re-read with FOR UPDATE to confirm the fine hasn't moved since
	// the read query, then UPDATE conditionally — Phase-4 may add a
	// retry queue, but this idempotency guard is the floor.
	var realStage int
	if err := tx.QueryRow(ctx,
		`SELECT escalation_stage FROM fines WHERE id=$1 FOR UPDATE`,
		fineID).Scan(&realStage); err != nil {
		return err
	}
	if realStage >= toStage {
		return nil // someone else got there first
	}

	newStatus := statusFor(action)
	if newStatus == "" {
		// Unknown action; keep status, just bump stage.
		if _, err := tx.Exec(ctx,
			`UPDATE fines SET escalation_stage=$2 WHERE id=$1`,
			fineID, toStage); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx,
			`UPDATE fines SET escalation_stage=$2, status=$3::fine_status WHERE id=$1`,
			fineID, toStage, newStatus); err != nil {
			return err
		}
	}

	env := events.NewEnvelope("fines", tenantID, events.TypeFineEscalated, 1,
		events.FineEscalatedPayload{
			FineID:    fineID,
			FromStage: fromStage,
			ToStage:   toStage,
			Action:    action,
			NewStatus: newStatus,
		})
	if err := events.WriteOutbox(ctx, tx, env); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func statusFor(action string) string {
	switch action {
	case "warning":
		return "warned"
	case "penalty":
		return "warned"
	case "flag":
		return "overdue"
	case "seize":
		return "seized"
	case "court":
		return "court"
	}
	return ""
}

// pgxConn is the small subset of pgx we depend on. *pgxpool.Conn
// satisfies it; tests can substitute their own.
type pgxConn interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}
