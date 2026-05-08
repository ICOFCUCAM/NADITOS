// Package worker drains the "inspection" retry queue and calls the
// bound provider for each tenant. Mirrors services/insurance/internal/
// worker — Phase-4 may consolidate them behind a shared
// connectors.RetryQueueDrainer.
package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

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
		// Phase-2: persist record + emit inspection.expired event.
		_ = rec
		return nil
	default:
		w.log.Warn("inspection: unknown job kind", "kind", job.Kind)
		return nil
	}
}
