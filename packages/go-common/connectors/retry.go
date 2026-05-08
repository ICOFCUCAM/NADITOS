package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RetryQueue is a DB-backed at-least-once job queue used by every
// integration that needs to call out to a flaky upstream:
//
//   queue.Enqueue("insurance.verify", payload)
//
// A worker calls Claim → run handler → Done(ok) / Done(err). On
// failure the job is rescheduled with exponential backoff up to
// max_attempts, then DLQ'd ("dead_letter") for human review.
type RetryQueue struct {
	pool *pgxpool.Pool
}

func NewRetryQueue(pool *pgxpool.Pool) *RetryQueue {
	return &RetryQueue{pool: pool}
}

// Enqueue inserts a new job. Module is a free-form bucket
// ("insurance","court","payments"); kind is the action verb
// ("verify","file","refund"). Payload is opaque JSON.
func (q *RetryQueue) Enqueue(ctx context.Context, tenantID, module, kind string, payload any, maxAttempts int) (uuid.UUID, error) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}
	conn, err := q.pool.Acquire(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = conn.QueryRow(ctx,
		`INSERT INTO retry_jobs (tenant_id, module, kind, payload, max_attempts)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		tenantID, module, kind, body, maxAttempts).Scan(&id)
	return id, err
}

// Job is what handlers receive.
type Job struct {
	ID        uuid.UUID
	TenantID  string
	Module    string
	Kind      string
	Payload   json.RawMessage
	Attempts  int
}

// Claim atomically grabs the next due job for a given module. Returns
// (nil, nil) when there's nothing to do. Multiple workers can claim
// safely (FOR UPDATE SKIP LOCKED).
func (q *RetryQueue) Claim(ctx context.Context, module string) (*Job, error) {
	conn, err := q.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return nil, err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var j Job
	err = tx.QueryRow(ctx,
		`SELECT id, tenant_id, module, kind, payload, attempts
		   FROM retry_jobs
		  WHERE status='queued' AND module=$1 AND next_run_at <= now()
		  ORDER BY next_run_at
		   FOR UPDATE SKIP LOCKED LIMIT 1`, module).
		Scan(&j.ID, &j.TenantID, &j.Module, &j.Kind, &j.Payload, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE retry_jobs SET status='running', updated_at=now() WHERE id=$1`,
		j.ID); err != nil {
		return nil, err
	}
	return &j, tx.Commit(ctx)
}

// Done marks the job complete or reschedules with exponential backoff.
// Backoff: 30s, 2m, 8m, 30m, 2h (capped). After max_attempts → dead_letter.
func (q *RetryQueue) Done(ctx context.Context, jobID uuid.UUID, runErr error) error {
	conn, err := q.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return err
	}
	if runErr == nil {
		_, err := conn.Exec(ctx,
			`UPDATE retry_jobs SET status='done', last_error=NULL, updated_at=now()
			   WHERE id=$1`, jobID)
		return err
	}
	// Failure path: read attempts/max, decide.
	var attempts, max int
	if err := conn.QueryRow(ctx,
		`SELECT attempts, max_attempts FROM retry_jobs WHERE id=$1`, jobID).
		Scan(&attempts, &max); err != nil {
		return err
	}
	attempts++
	status := "queued"
	if attempts >= max {
		status = "dead_letter"
	}
	delay := backoffFor(attempts)
	_, err = conn.Exec(ctx,
		`UPDATE retry_jobs
		    SET status=$2::retry_job_status,
		        attempts=$3,
		        next_run_at=now()+ make_interval(secs => $4),
		        last_error=$5,
		        updated_at=now()
		  WHERE id=$1`,
		jobID, status, attempts, int(delay.Seconds()), runErr.Error())
	return err
}

func backoffFor(attempts int) time.Duration {
	switch attempts {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	case 3:
		return 8 * time.Minute
	case 4:
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}
