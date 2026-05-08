// Package demerit owns the rules that translate violations into license
// points, and points into automatic suspensions.
//
// Flow:
//
//   fine.issued (carrying driver_license_id)
//        │
//        ▼
//   demerit.applyForFine
//      • read regulation_offences.points (per-tenant)
//      • append driver_demerit_events row
//      • recompute denormalized driver_licenses.points
//      • if points >= driver_demerit_policy.threshold_points
//        within window_months, open driver_suspensions
//      • emit license.demerit and (optionally) license.suspended
package demerit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/events"
)

type Engine struct {
	pool  *pgxpool.Pool
	log   *slog.Logger
	audit *audit.Client
	bus   events.Publisher
}

func New(pool *pgxpool.Pool, log *slog.Logger, audit *audit.Client, bus events.Publisher) *Engine {
	return &Engine{pool: pool, log: log, audit: audit, bus: bus}
}

// Wire registers the engine as a subscriber on the in-process bus. With a
// transport bus the same handler is the body of a JetStream consumer.
func (e *Engine) Wire(sub events.Subscriber) {
	_ = sub.Subscribe(events.TypeFineIssued, e.onFineIssued)
}

func (e *Engine) onFineIssued(ctx context.Context, env events.Envelope) error {
	// We need the driver_license_id; the fine.issued payload doesn't carry
	// it because plate-only fines exist. Look up via fine_id.
	p, err := decodeFineIssued(env.Data)
	if err != nil {
		return err
	}
	return e.applyForFine(ctx, env.TenantID, p.FineID)
}

func (e *Engine) applyForFine(ctx context.Context, tenantID, fineID string) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		licenseID    *uuid.UUID
		offenceCode  string
		points       int
	)
	err = tx.QueryRow(ctx,
		`SELECT f.driver_license_id, f.offence_code,
		        COALESCE((SELECT (manifest->'offences'->f.offence_code->>'points')::int
		                    FROM country_packs cp
		                    JOIN tenant_country_pack tcp ON tcp.pack_id = cp.id
		                   WHERE tcp.tenant_id = f.tenant_id
		                   LIMIT 1), 0)
		   FROM fines f WHERE f.id = $1 AND f.tenant_id = $2`,
		fineID, tenantID).Scan(&licenseID, &offenceCode, &points)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // fine vanished — nothing to do
	}
	if err != nil {
		return err
	}
	if licenseID == nil || points == 0 {
		return nil // no driver attached, or zero-point offence
	}

	// Append demerit ledger entry.
	if _, err := tx.Exec(ctx,
		`INSERT INTO driver_demerit_events
		   (tenant_id, license_id, delta, reason, source, source_id)
		 VALUES ($1,$2,$3,$4,'fine',$5)`,
		tenantID, *licenseID, points, "fine:"+offenceCode, fineID); err != nil {
		return err
	}

	// Recompute total within the policy window.
	var threshold, window, suspMonths int
	_ = tx.QueryRow(ctx,
		`SELECT threshold_points, window_months, suspension_months
		   FROM driver_demerit_policy WHERE tenant_id=$1`,
		tenantID).Scan(&threshold, &window, &suspMonths)
	if threshold == 0 {
		threshold, window, suspMonths = 12, 24, 6 // safe defaults
	}

	var newTotal int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta),0) FROM driver_demerit_events
		   WHERE license_id=$1 AND occurred_at > now() - ($2 || ' months')::interval`,
		*licenseID, window).Scan(&newTotal); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE driver_licenses SET points=$2 WHERE id=$1`, *licenseID, newTotal); err != nil {
		return err
	}

	// Trigger automatic suspension if over threshold.
	suspended := false
	if newTotal >= threshold {
		var existing int
		_ = tx.QueryRow(ctx,
			`SELECT count(*) FROM driver_suspensions
			   WHERE license_id=$1 AND ends_at > now() AND lifted_at IS NULL`,
			*licenseID).Scan(&existing)
		if existing == 0 {
			ends := time.Now().Add(time.Duration(suspMonths) * 30 * 24 * time.Hour)
			if _, err := tx.Exec(ctx,
				`INSERT INTO driver_suspensions
				   (tenant_id, license_id, reason, trigger_kind, starts_at, ends_at)
				 VALUES ($1,$2,$3,'demerit', now(), $4)`,
				tenantID, *licenseID,
				"demerit threshold reached", ends); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE driver_licenses SET is_suspended=true, suspended_until=$2
				   WHERE id=$1`, *licenseID, ends); err != nil {
				return err
			}
			suspended = true
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	e.publish(ctx, tenantID, *licenseID, points, fineID, offenceCode, newTotal, suspended, suspMonths)
	return nil
}

func (e *Engine) publish(ctx context.Context, tenant string, lid uuid.UUID,
	delta int, fineID, offenceCode string, newTotal int, suspended bool, suspMonths int) {
	dem := events.NewEnvelope("license", tenant, events.TypeLicenseDemerit, 1,
		events.LicenseDemeritPayload{
			LicenseID: lid.String(), Delta: delta,
			Reason: "fine:" + offenceCode, Source: "fine", SourceID: fineID,
			NewTotal: newTotal,
		})
	_ = e.bus.Publish(ctx, dem)
	if suspended {
		susp := events.NewEnvelope("license", tenant, events.TypeLicenseSuspended, 1,
			events.LicenseSuspendedPayload{
				LicenseID: lid.String(), Reason: "demerit threshold reached",
				TriggerKind: "demerit",
				StartsAt:    time.Now().UTC().Format(time.RFC3339),
				EndsAt:      time.Now().AddDate(0, suspMonths, 0).UTC().Format(time.RFC3339),
			})
		_ = e.bus.Publish(ctx, susp)
	}
	_ = e.audit.Emit(ctx, "license.demerit", "license", lid.String(), nil, map[string]any{
		"delta": delta, "fine_id": fineID, "new_total": newTotal, "suspended": suspended,
	})
}

// decodeFineIssued is forgiving — the bus delivers either the typed
// payload struct or its JSON form depending on transport.
func decodeFineIssued(raw any) (*events.FineIssuedPayload, error) {
	switch v := raw.(type) {
	case events.FineIssuedPayload:
		return &v, nil
	case *events.FineIssuedPayload:
		return v, nil
	case map[string]any:
		fid, _ := v["fine_id"].(string)
		return &events.FineIssuedPayload{FineID: fid}, nil
	}
	return nil, errors.New("demerit: unrecognized fine.issued payload type")
}
