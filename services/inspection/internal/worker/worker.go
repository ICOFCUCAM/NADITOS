// Package worker drains the "inspection" retry queue and calls the
// bound provider for each tenant. Mirrors services/insurance/internal/
// worker — Phase-4 may consolidate them behind a shared
// connectors.RetryQueueDrainer.
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
	"github.com/icofcucam/naditos/packages/go-common/contracts/inspection"
)

type Worker struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	router *connectors.CountryRouter[inspection.Verifier]
	health *connectors.HealthMonitor
	queue  *connectors.RetryQueue
}

func New(pool *pgxpool.Pool, log *slog.Logger,
	router *connectors.CountryRouter[inspection.Verifier],
	health *connectors.HealthMonitor, queue *connectors.RetryQueue) *Worker {
	return &Worker{pool: pool, log: log, router: router,
		health: health, queue: queue}
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
	job, err := w.queue.Claim(ctx, "inspection")
	if err != nil {
		w.log.Warn("inspection: claim failed", "err", err)
		return
	}
	if job == nil {
		return
	}
	if err := w.run(ctx, job); err != nil {
		if dErr := w.queue.Done(ctx, job.ID, err); dErr != nil {
			w.log.Warn("inspection: done(failure) failed", "err", dErr)
		}
		return
	}
	if dErr := w.queue.Done(ctx, job.ID, nil); dErr != nil {
		w.log.Warn("inspection: done(ok) failed", "err", dErr)
	}
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
		rec, err := prov.VerifyByPlate(ctx, job.TenantID, p.Plate)
		if err != nil {
			_ = w.health.Fail(ctx, job.TenantID, info.Module, info.Provider, info.Region, err.Error())
			return err
		}
		_ = w.health.OK(ctx, job.TenantID, info.Module, info.Provider, info.Region, nil)
		// Persist the record + update the vehicle's inspection_expires_at
		// so the next compliance lookup sees the fresh data without a
		// re-verify. nil rec → provider has no record on file (e.g. plate
		// not registered with this jurisdiction); we leave the vehicle
		// alone and let the next reconcile sweep retry.
		if rec != nil {
			if err := w.persistInspection(ctx, job.TenantID, p.Plate, rec); err != nil {
				w.log.Warn("inspection: persist failed",
					slog.String("plate", p.Plate),
					slog.String("err", err.Error()))
				return err
			}
		}
		return nil
	default:
		w.log.Warn("inspection: unknown job kind", "kind", job.Kind)
		return nil
	}
}

// persistInspection writes the inspection_records row and updates
// vehicles.inspection_expires_at atomically. If the vehicle isn't in
// our registry (plate unknown to this tenant), we silently skip — the
// provider answered for a plate we don't track, which is a no-op as
// far as our compliance state is concerned.
func (w *Worker) persistInspection(ctx context.Context, tenant, plate string, rec *inspection.Record) error {
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
		`INSERT INTO inspection_records
		   (tenant_id, vehicle_id, station, performed_at, expires_at, result, certificate_url)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)`,
		tenant, vid, rec.Station, rec.PerformedAt, rec.ExpiresAt,
		rec.Result, rec.CertificateURL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE vehicles SET inspection_expires_at=$2 WHERE id=$1::uuid`,
		vid, rec.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
