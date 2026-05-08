// Package worker drains the "insurance" retry queue and calls the bound
// provider for each tenant.
package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

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
		// Phase-2: persist policy + emit insurance.expired/renewed events.
		_ = policy
		return nil
	default:
		// Unknown kind — DLQ it after backoff.
		w.log.Warn("insurance: unknown job kind", "kind", job.Kind)
		return nil
	}
}
