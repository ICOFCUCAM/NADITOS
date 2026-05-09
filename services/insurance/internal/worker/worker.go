// Package worker drains the "insurance" retry queue and calls the bound
// provider for each tenant.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts/insurance"
	"github.com/icofcucam/naditos/packages/go-common/events"
)

type Worker struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	router *connectors.CountryRouter[insurance.Verifier]
	health *connectors.HealthMonitor
	queue  *connectors.RetryQueue
	bus    events.Publisher
}

func New(pool *pgxpool.Pool, log *slog.Logger,
	router *connectors.CountryRouter[insurance.Verifier],
	health *connectors.HealthMonitor, queue *connectors.RetryQueue,
	bus events.Publisher) *Worker {
	return &Worker{pool: pool, log: log, router: router,
		health: health, queue: queue, bus: bus}
}

func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	job, err := w.queue.Claim(ctx, "insurance")
	if err != nil {
		w.log.Warn("insurance: claim failed", "err", err)
		return
	}
	if job == nil {
		return
	}
	if err := w.run(ctx, job); err != nil {
		_ = w.queue.Done(ctx, job.ID, err)
		return
	}
	_ = w.queue.Done(ctx, job.ID, nil)
}

func (w *Worker) run(ctx context.Context, job *connectors.Job) error {
	prov, err := w.router.For(job.TenantID)
	if err != nil {
		return err
	}
	info := prov.Info()

	switch job.Kind {
	case "verify":
		var p struct {
			VehicleID string `json:"vehicle_id"`
			Plate     string `json:"plate"`
		}
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		policy, err := prov.VerifyByPlate(ctx, job.TenantID, p.Plate)
		if err != nil {
			_ = w.health.Fail(ctx, job.TenantID, info.Module, info.Provider, info.Region, err.Error())
			return err
		}
		_ = w.health.OK(ctx, job.TenantID, info.Module, info.Provider, info.Region, nil)
		// Persist the policy + update the vehicle's insurance_expires_at
		// so the next compliance lookup reflects the fresh data without
		// re-verifying. nil policy → no record on file (vehicle is
		// uninsured per the bureau); leave the row alone.
		if policy != nil {
			if err := w.persistPolicy(ctx, job.TenantID, p.Plate, policy); err != nil {
				w.log.Warn("insurance: persist failed",
					slog.String("plate", p.Plate),
					slog.String("err", err.Error()))
				return err
			}
		}
		return nil
	default:
		// Unknown kind — DLQ it after backoff.
		w.log.Warn("insurance: unknown job kind", "kind", job.Kind)
		return nil
	}
}

// persistPolicy writes the insurance_records row and updates
// vehicles.insurance_expires_at atomically. Plate not in our registry
// is silently skipped — the bureau answered for a plate we don't track.
func (w *Worker) persistPolicy(ctx context.Context, tenant, plate string, p *insurance.Policy) error {
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var vid string
	if err := tx.QueryRow(ctx,
		`SELECT id FROM vehicles WHERE tenant_id=$1 AND plate=$2`,
		tenant, plate).Scan(&vid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tx.Commit(ctx) // unknown plate; not an error
		}
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO insurance_records
		   (tenant_id, vehicle_id, provider, policy_number,
		    starts_at, expires_at, is_active)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)`,
		tenant, vid, p.Provider, p.PolicyNumber,
		p.StartsAt, p.ExpiresAt, p.IsActive); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE vehicles SET insurance_expires_at=$2 WHERE id=$1::uuid`,
		vid, p.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
