// Package connectors provides shared infrastructure for every external
// provider integration: a retry queue (DB-backed), a health monitor, and
// a per-tenant country router. Insurance, inspection, court, payments,
// and identity adapters all use these so the operational shape is
// identical across modules.
package connectors

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthState mirrors the provider_health.state column.
type HealthState string

const (
	HealthOK       HealthState = "ok"
	HealthDegraded HealthState = "degraded"
	HealthDown     HealthState = "down"
	HealthUnknown  HealthState = "unknown"
)

// HealthMonitor tracks provider success/failure streaks and persists the
// computed state to provider_health. Workers call OK / Fail; a dashboard
// reads the table.
type HealthMonitor struct {
	pool *pgxpool.Pool
}

func NewHealthMonitor(pool *pgxpool.Pool) *HealthMonitor {
	return &HealthMonitor{pool: pool}
}

func (h *HealthMonitor) OK(ctx context.Context, tenantID, module, provider, region string, details map[string]any) error {
	d, _ := json.Marshal(details)
	_, err := h.exec(ctx,
		`INSERT INTO provider_health (tenant_id, module, provider, region,
		                              last_ok_at, fail_streak, state, details)
		 VALUES ($1,$2,$3,$4, now(), 0, 'ok', $5)
		 ON CONFLICT (tenant_id, module, provider) DO UPDATE
		   SET last_ok_at=now(), fail_streak=0, state='ok',
		       details=EXCLUDED.details, updated_at=now()`,
		tenantID, module, provider, nullStr(region), d)
	return err
}

func (h *HealthMonitor) Fail(ctx context.Context, tenantID, module, provider, region string, errMsg string) error {
	d, _ := json.Marshal(map[string]string{"error": errMsg})
	_, err := h.exec(ctx,
		`INSERT INTO provider_health (tenant_id, module, provider, region,
		                              last_fail_at, fail_streak, state, details)
		 VALUES ($1,$2,$3,$4, now(), 1, 'degraded', $5)
		 ON CONFLICT (tenant_id, module, provider) DO UPDATE
		   SET last_fail_at=now(),
		       fail_streak=provider_health.fail_streak + 1,
		       state = CASE
		         WHEN provider_health.fail_streak + 1 >= 5 THEN 'down'
		         ELSE 'degraded'
		       END,
		       details=EXCLUDED.details, updated_at=now()`,
		tenantID, module, provider, nullStr(region), d)
	return err
}

// Snapshot returns the current state for one provider.
func (h *HealthMonitor) Snapshot(ctx context.Context, tenantID, module, provider string) (HealthState, time.Time, time.Time, int, error) {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return HealthUnknown, time.Time{}, time.Time{}, 0, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return HealthUnknown, time.Time{}, time.Time{}, 0, err
	}
	var (
		state         string
		lastOK, lastF *time.Time
		streak        int
	)
	err = conn.QueryRow(ctx,
		`SELECT state, last_ok_at, last_fail_at, fail_streak
		   FROM provider_health
		  WHERE tenant_id=$1 AND module=$2 AND provider=$3`,
		tenantID, module, provider).Scan(&state, &lastOK, &lastF, &streak)
	if err != nil {
		return HealthUnknown, time.Time{}, time.Time{}, 0, err
	}
	o, f := time.Time{}, time.Time{}
	if lastOK != nil {
		o = *lastOK
	}
	if lastF != nil {
		f = *lastF
	}
	return HealthState(state), o, f, streak, nil
}

// exec runs a write with row_security off — provider_health crosses
// modules but stays per-tenant via the tenant_id column.
func (h *HealthMonitor) exec(ctx context.Context, sql string, args ...any) (any, error) {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return nil, err
	}
	return conn.Exec(ctx, sql, args...)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
