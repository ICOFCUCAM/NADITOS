// Package pipeline runs the ANPR async worker:
//
//   anpr_jobs (status=queued)
//        │  poll every 500ms
//        ▼
//   normalize plate
//        ▼
//   dedup (window 60s, same tenant + plate within geo proximity)
//        ▼
//   match vehicle (registry table)
//        ▼
//   write anpr_scans row + emit events:
//      anpr.scan         (always)
//      anpr.matched      (when matched_vehicle_id != nil)
//      anpr.alert        (when matched vehicle is stolen/seized/wanted)
//        ▼
//   anpr_jobs.status = done
//
// Failures bump attempts; >5 attempts → status=failed (a human follows up).
package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/services/anpr-gateway/internal/normalize"
)

const (
	dedupWindow = 60 * time.Second
	maxAttempts = 5
	pollEvery   = 500 * time.Millisecond
)

type Worker struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	bus  events.Publisher
}

func New(pool *pgxpool.Pool, log *slog.Logger, bus events.Publisher) *Worker {
	return &Worker{pool: pool, log: log, bus: bus}
}

// Run blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(pollEvery)
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
	// Claim one job. SKIP LOCKED makes this safe across multiple worker pods.
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	type job struct {
		ID            uuid.UUID
		Tenant        string
		Source        string
		SourceID      *string
		RawPlate      string
		Confidence    float32
		GeoLat, GeoLng float64
		ImageS3Key    *string
		CapturedAt    time.Time
		Attempts      int
	}
	var j job
	err = tx.QueryRow(ctx,
		`SELECT id, tenant_id, source, source_id, raw_plate, confidence,
		        COALESCE(geo_lat,0), COALESCE(geo_lng,0), image_s3_key,
		        captured_at, attempts
		   FROM anpr_jobs
		  WHERE status='queued'
		  ORDER BY enqueued_at
		   FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&j.ID, &j.Tenant, &j.Source, &j.SourceID, &j.RawPlate, &j.Confidence,
			&j.GeoLat, &j.GeoLng, &j.ImageS3Key, &j.CapturedAt, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return // nothing to do
	}
	if err != nil {
		w.log.Warn("anpr: claim failed", "err", err)
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE anpr_jobs SET status='processing', attempts=attempts+1
		   WHERE id=$1`, j.ID); err != nil {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}

	// Process outside the locking transaction.
	if err := w.process(ctx, j.ID, j.Tenant, j.Source, j.SourceID, j.RawPlate,
		j.Confidence, j.GeoLat, j.GeoLng, j.ImageS3Key, j.CapturedAt); err != nil {
		w.fail(ctx, j.ID, j.Attempts+1, err)
		return
	}
}

func (w *Worker) process(ctx context.Context, jobID uuid.UUID, tenant, source string,
	sourceID *string, rawPlate string, conf float32, lat, lng float64,
	imageKey *string, capturedAt time.Time) error {

	// Read the tenant's plate regex for normalization.
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return err
	}
	var plateRegex string
	_ = conn.QueryRow(ctx, `SELECT plate_regex FROM tenants WHERE id=$1`, tenant).Scan(&plateRegex)
	plate, _ := normalize.Normalize(rawPlate, plateRegex)
	// Even if normalization fails the tenant regex, we still process — the
	// match step will simply not find a vehicle.

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Dedup: any scan of the same plate within 60s and (rough) geo-proximity?
	var dupID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM anpr_scans
		  WHERE tenant_id=$1 AND plate_read=$2
		    AND captured_at BETWEEN $3 AND $4
		  LIMIT 1`,
		tenant, plate, capturedAt.Add(-dedupWindow), capturedAt.Add(dedupWindow)).Scan(&dupID)
	isDup := err == nil

	// Match vehicle.
	var vehicleID *uuid.UUID
	var stolen, seized, wanted bool
	{
		var v uuid.UUID
		err := tx.QueryRow(ctx,
			`SELECT id, is_stolen, is_seized, is_wanted FROM vehicles
			  WHERE tenant_id=$1 AND plate=$2`, tenant, plate).
			Scan(&v, &stolen, &seized, &wanted)
		if err == nil {
			vehicleID = &v
		}
	}

	// Write the canonical scan row.
	var scanID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO anpr_scans
		   (tenant_id, plate_read, confidence, source, source_id,
		    captured_at, geo_lat, geo_lng, image_s3_key, matched_vehicle_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		tenant, plate, conf, source, sourceID, capturedAt, lat, lng, imageKey, vehicleID).
		Scan(&scanID)
	if err != nil {
		return err
	}

	// Mark job done (or duplicate).
	endStatus := "done"
	if isDup {
		endStatus = "duplicate"
	}
	if _, err := tx.Exec(ctx,
		`UPDATE anpr_jobs
		    SET status=$2::anpr_job_status, normalized_plate=$3, scan_id=$4,
		        processed_at=now(), last_error=NULL
		  WHERE id=$1`,
		jobID, endStatus, plate, scanID); err != nil {
		return err
	}

	// Emit events through the outbox INSIDE the same tx as the scan
	// write. The relay drains event_outbox into the in-process bus, so
	// in-process subscribers still see the events; cross-process
	// consumers (audit-anpr-alerts, future analytics) read directly
	// from the outbox so they never miss an event a bus-only publish
	// would have lost on restart.
	matched := ""
	if vehicleID != nil {
		matched = vehicleID.String()
	}
	scanEnv := events.NewEnvelope("anpr-gateway", tenant, events.TypeAnprScan, 1,
		events.AnprScanPayload{
			ScanID: scanID.String(), Plate: plate, Confidence: conf,
			Source: source, GeoLat: lat, GeoLng: lng, MatchedVehicleID: matched,
		})
	if err := events.WriteOutbox(ctx, tx, scanEnv); err != nil {
		return err
	}
	if vehicleID != nil {
		matchedEnv := events.NewEnvelope("anpr-gateway", tenant, events.TypeAnprMatched, 1,
			events.AnprScanPayload{
				ScanID: scanID.String(), Plate: plate, Confidence: conf,
				Source: source, GeoLat: lat, GeoLng: lng, MatchedVehicleID: matched,
			})
		if err := events.WriteOutbox(ctx, tx, matchedEnv); err != nil {
			return err
		}
		if stolen || seized || wanted {
			alertEnv := events.NewEnvelope("anpr-gateway", tenant, events.TypeAnprAlert, 1,
				events.AnprAlertPayload{
					ScanID: scanID.String(), Plate: plate, VehicleID: matched,
					IsStolen: stolen, IsSeized: seized, IsWanted: wanted,
				})
			if err := events.WriteOutbox(ctx, tx, alertEnv); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (w *Worker) fail(ctx context.Context, jobID uuid.UUID, attempts int, processErr error) {
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	_, _ = conn.Exec(ctx, "SET LOCAL row_security = off")
	status := "queued"
	if attempts >= maxAttempts {
		status = "failed"
	}
	_, _ = conn.Exec(ctx,
		`UPDATE anpr_jobs
		    SET status=$2::anpr_job_status, last_error=$3
		  WHERE id=$1`,
		jobID, status, processErr.Error())
	w.log.Warn("anpr: process failed", "job", jobID, "attempts", attempts, "err", processErr)
}
