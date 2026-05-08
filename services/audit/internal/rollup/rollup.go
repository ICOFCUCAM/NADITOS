// Package rollup recomputes officer_daily_stats and an anomaly score
// per (tenant, officer, day).
//
// Why it lives in the audit service: audit is the one component that
// already runs with a BYPASSRLS role and writes across tenants — exactly
// what an analytics rollup needs. The job is read-mostly against fines /
// fine_payments and write-only against officer_daily_stats. Phase-4 may
// promote it to a standalone "analytics" service when load demands it;
// the package boundary is already clean.
//
// Aggregation:
//
//	per (tenant, officer, day):
//	  fines_issued    = count(*)            from fines    WHERE issued_by=officer
//	  fines_cancelled = count(status=cancelled)
//	  fines_total     = sum(amount) excluding cancelled
//	  scans_run       — Phase-4 (anpr_scans doesn't yet record officer)
//	  unique_plates   = count(distinct plate)
//
// Anomaly score:
//
//	A within-officer z-score of fines_issued against the officer's prior
//	14-day rolling mean and stddev. Officers with <3 prior days get NULL
//	(insufficient data). A score of 0 means "right at the mean"; anything
//	above 2.0 is a red flag the dashboard surfaces.
//
//	We expose the raw z-score and let the UI translate that to a 0..1
//	band — keeps the data lossless. The DB column is REAL so we store
//	either the z-score directly or NULL.
package rollup

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Job struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	every  time.Duration
	window time.Duration // how far back from now() to recompute
}

// New returns a Job. Default sweep is 60 minutes for the schedule and
// 14 days for the recomputation window.
func New(pool *pgxpool.Pool, log *slog.Logger) *Job {
	return &Job{pool: pool, log: log, every: 60 * time.Minute, window: 14 * 24 * time.Hour}
}

func (j *Job) WithSchedule(every time.Duration) *Job { j.every = every; return j }

// Run blocks until ctx cancels. The first sweep happens immediately so
// dashboards aren't empty after a restart.
func (j *Job) Run(ctx context.Context) {
	if err := j.RunOnce(ctx); err != nil {
		j.log.Warn("rollup: initial sweep failed", "err", err)
	}
	t := time.NewTicker(j.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := j.RunOnce(ctx); err != nil {
				j.log.Warn("rollup: sweep failed", "err", err)
			}
		}
	}
}

// RunOnce performs one full pass: aggregate the window, score, and
// emit anomaly alerts for outliers. Exposed so tests and the on-demand
// admin trigger can drive it.
func (j *Job) RunOnce(ctx context.Context) error {
	start := time.Now()
	from := time.Now().Add(-j.window).UTC().Format("2006-01-02")
	if err := aggregate(ctx, j.pool, from); err != nil {
		return err
	}
	if err := score(ctx, j.pool, from); err != nil {
		return err
	}
	if err := detectAnomalies(ctx, j.pool, from); err != nil {
		return err
	}
	j.log.Info("rollup swept",
		slog.String("from", from),
		slog.Duration("dur", time.Since(start)))
	return nil
}

// aggregate (re)computes officer_daily_stats for every (tenant, officer,
// day) in the window. UPSERT keeps it idempotent so multiple replicas
// or repeated runs don't duplicate rows.
//
// The query joins fines.issued_by → officer_id; days where an officer
// issued zero fines simply don't get a row (we track only active days).
func aggregate(ctx context.Context, pool *pgxpool.Pool, from string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, `
		INSERT INTO officer_daily_stats
		   (tenant_id, officer_id, day, fines_issued, fines_cancelled, fines_total, unique_plates)
		SELECT
		   f.tenant_id,
		   f.issued_by,
		   (f.issued_at AT TIME ZONE 'UTC')::date AS day,
		   COUNT(*)                                                  AS fines_issued,
		   COUNT(*) FILTER (WHERE f.status = 'cancelled')            AS fines_cancelled,
		   COALESCE(SUM(f.amount) FILTER (WHERE f.status <> 'cancelled'), 0) AS fines_total,
		   COUNT(DISTINCT f.plate)                                   AS unique_plates
		  FROM fines f
		 WHERE f.issued_at >= $1::date
		 GROUP BY f.tenant_id, f.issued_by, day
		ON CONFLICT (tenant_id, officer_id, day) DO UPDATE
		   SET fines_issued    = EXCLUDED.fines_issued,
		       fines_cancelled = EXCLUDED.fines_cancelled,
		       fines_total     = EXCLUDED.fines_total,
		       unique_plates   = EXCLUDED.unique_plates`, from)
	return err
}

// score updates anomaly_score for every (tenant, officer, day) in the
// window. The score is the z-score of fines_issued against the same
// officer's prior 14 days (excluding the day being scored).
//
// SQL trick: a window function ranges 14 PRECEDING through 1 PRECEDING,
// computing mean and stddev_pop over the last 14 active days. NULL out
// when there are <3 prior days OR when stddev is 0 (constant series).
func score(ctx context.Context, pool *pgxpool.Pool, from string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, `
		WITH baseline AS (
		  SELECT
		     tenant_id, officer_id, day, fines_issued,
		     AVG(fines_issued::float)
		       OVER (PARTITION BY tenant_id, officer_id
		             ORDER BY day
		             ROWS BETWEEN 14 PRECEDING AND 1 PRECEDING) AS mu,
		     STDDEV_POP(fines_issued::float)
		       OVER (PARTITION BY tenant_id, officer_id
		             ORDER BY day
		             ROWS BETWEEN 14 PRECEDING AND 1 PRECEDING) AS sigma,
		     COUNT(*)
		       OVER (PARTITION BY tenant_id, officer_id
		             ORDER BY day
		             ROWS BETWEEN 14 PRECEDING AND 1 PRECEDING) AS n
		    FROM officer_daily_stats
		   WHERE day >= $1::date - INTERVAL '14 days'
		)
		UPDATE officer_daily_stats s
		   SET anomaly_score = CASE
		     WHEN b.n < 3 OR b.sigma IS NULL OR b.sigma = 0 THEN NULL
		     ELSE ((b.fines_issued - b.mu) / b.sigma)::real
		   END
		  FROM baseline b
		 WHERE s.tenant_id  = b.tenant_id
		   AND s.officer_id = b.officer_id
		   AND s.day        = b.day
		   AND s.day        >= $1::date`, from)
	return err
}

// detectAnomalies materializes audit_alerts rows from the rollup
// outputs. Two detectors run today; each is idempotent against
// audit_alerts_uniq_open_idx so a re-sweep on the same data doesn't
// duplicate signals.
//
//   officer_high_anomaly_z   — z-score above HighAnomalyZThreshold
//                              against the officer's own 14d baseline
//   officer_high_cancel_rate — fines_cancelled / fines_issued above
//                              HighCancelRateThreshold AND ≥ minimum
//                              activity, so a single-fine day where
//                              that fine was cancelled doesn't fire
//
// Severity stamps the worst observed value so the dashboard can
// gradient-shade the alert without reading details.
//
// The CONFLICT clause matches the open-only partial unique index in
// the migration: an already-OPEN alert is left alone (DO NOTHING),
// while a previously-resolved alert is allowed to re-fire as a fresh
// row when the bad behaviour returns.
func detectAnomalies(ctx context.Context, pool *pgxpool.Pool, from string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `
		INSERT INTO audit_alerts
		   (tenant_id, kind, subject_kind, subject_id, day, severity, details)
		SELECT s.tenant_id, 'officer_high_anomaly_z', 'officer',
		       s.officer_id, s.day, s.anomaly_score,
		       jsonb_build_object('z', s.anomaly_score,
		                          'fines_issued', s.fines_issued)
		  FROM officer_daily_stats s
		 WHERE s.day >= $1::date
		   AND s.anomaly_score IS NOT NULL
		   AND s.anomaly_score > $2
		ON CONFLICT DO NOTHING`, from, HighAnomalyZThreshold); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, `
		INSERT INTO audit_alerts
		   (tenant_id, kind, subject_kind, subject_id, day, severity, details)
		SELECT s.tenant_id, 'officer_high_cancel_rate', 'officer',
		       s.officer_id, s.day,
		       (s.fines_cancelled::real / NULLIF(s.fines_issued,0)::real)::real,
		       jsonb_build_object(
		         'fines_issued', s.fines_issued,
		         'fines_cancelled', s.fines_cancelled,
		         'rate', round(
		           (s.fines_cancelled::numeric / NULLIF(s.fines_issued,0)::numeric)::numeric,
		           3))
		  FROM officer_daily_stats s
		 WHERE s.day >= $1::date
		   AND s.fines_issued >= $2
		   AND s.fines_cancelled::real / s.fines_issued::real > $3
		ON CONFLICT DO NOTHING`, from, MinActivityForCancelRate, HighCancelRateThreshold); err != nil {
		return err
	}
	return nil
}

// Tuning knobs. Values tracked here so a future per-tenant override
// can replace the constants without touching the SQL above.
const (
	HighAnomalyZThreshold    = 2.0
	HighCancelRateThreshold  = 0.30
	MinActivityForCancelRate = 5
)
