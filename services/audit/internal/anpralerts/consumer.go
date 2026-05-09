// Package anpralerts consumes anpr.alert events and materializes them
// into audit_alerts so dispatch / admin staff see flagged-vehicle
// matches alongside other anomaly signals on the /audit dashboard.
//
// Why audit_alerts and not a dedicated table: the surface (list +
// resolve + severity + details JSON) is already built and the partial
// unique index on (tenant, kind, subject, day) WHERE resolved_at IS
// NULL gives us idempotency for free. A flagged vehicle scanned five
// times in one day is one open alert, not five rows of noise.
package anpralerts

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/events"
)

const (
	// AlertKind is the audit_alerts.kind value the consumer writes.
	AlertKind = "anpr_match_flagged_vehicle"
)

// Run is a blocking helper that wires a consumer onto the standard
// event_outbox stream for anpr.alert and materializes each one into
// audit_alerts. Caller passes ctx; cancel to stop.
func Run(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	c := events.NewConsumer(pool, log, "audit-anpr-alerts",
		func(ctx context.Context, env events.Envelope) error {
			return handle(ctx, pool, env)
		},
	).OnlyTypes(events.TypeAnprAlert)
	c.Run(ctx)
}

// handle is exported via the lowercased name only for tests; production
// goes through Run.
func handle(ctx context.Context, pool *pgxpool.Pool, env events.Envelope) error {
	var p events.AnprAlertPayload
	if err := decodeData(env.Data, &p); err != nil {
		return err
	}
	// Severity = number of flags set. A vehicle that's stolen + wanted
	// is louder than one that's only seized.
	sev := float32(0)
	flags := []string{}
	if p.IsStolen {
		sev += 1
		flags = append(flags, "stolen")
	}
	if p.IsSeized {
		sev += 1
		flags = append(flags, "seized")
	}
	if p.IsWanted {
		sev += 1
		flags = append(flags, "wanted")
	}
	if sev == 0 {
		// Defensive: alert event with zero flags would be a producer
		// bug. Skip rather than write a meaningless audit_alerts row.
		return nil
	}

	details, _ := json.Marshal(map[string]any{
		"scan_id": p.ScanID,
		"plate":   p.Plate,
		"flags":   flags,
	})

	_, err := pool.Exec(ctx,
		`INSERT INTO audit_alerts
		   (tenant_id, kind, subject_kind, subject_id, day, severity, details)
		 VALUES ($1, $2, 'vehicle', $3::uuid, current_date, $4, $5)
		 ON CONFLICT DO NOTHING`,
		env.TenantID, AlertKind, p.VehicleID, sev, details)
	return err
}

// HandleForTest exposes handle for the package's own tests without
// exporting it more broadly.
func HandleForTest(ctx context.Context, pool *pgxpool.Pool, env events.Envelope) error {
	return handle(ctx, pool, env)
}

// decodeData mirrors the helper in services/notifications/internal/
// consumer: in-process bus delivers typed payloads, JSON-over-NATS
// delivers map[string]any. Round-tripping through json normalizes
// both into the typed struct callers want.
func decodeData(raw any, out any) error {
	if raw == nil {
		return errors.New("anpralerts: nil event data")
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}
