// Package api wires the insurance service's HTTP surface.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts/insurance"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type API struct {
	cfg    config.Service
	log    *slog.Logger
	pool   *pgxpool.Pool
	issuer *auth.Issuer
	router *connectors.CountryRouter[insurance.Verifier]
	health *connectors.HealthMonitor
	queue  *connectors.RetryQueue
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool, issuer *auth.Issuer,
	router *connectors.CountryRouter[insurance.Verifier],
	health *connectors.HealthMonitor, queue *connectors.RetryQueue) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, issuer: issuer,
		router: router, health: health, queue: queue}
	mux := http.NewServeMux()

	mux.Handle("GET /v1/insurance/verify",
		issuer.Middleware(http.HandlerFunc(a.verify)))
	mux.Handle("POST /v1/insurance/reconcile",
		issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(a.reconcile))))
	mux.Handle("GET /v1/insurance/health",
		issuer.Middleware(http.HandlerFunc(a.healthz)))
	// Webhooks are unauthenticated by JWT — provider-signed instead. We
	// don't sign-verify here in the dev stub; real adapters MUST.
	mux.HandleFunc("POST /v1/insurance/webhooks/{provider}", a.webhook)

	return mux
}

// GET /v1/insurance/verify?plate=... or ?vin=...
func (a *API) verify(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	plate := r.URL.Query().Get("plate")
	vin := r.URL.Query().Get("vin")
	if plate == "" && vin == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing", "plate or vin required"))
		return
	}

	prov, err := a.router.For(c.TenantID)
	if err != nil {
		httpx.WriteErr(w, httpx.Err(503, "no_provider", err.Error()))
		return
	}

	var policy *insurance.Policy
	var verr error
	if plate != "" {
		policy, verr = prov.VerifyByPlate(r.Context(), c.TenantID, plate)
	} else {
		policy, verr = prov.VerifyByVIN(r.Context(), c.TenantID, vin)
	}
	info := prov.Info()
	if verr != nil {
		_ = a.health.Fail(r.Context(), c.TenantID, info.Module, info.Provider, info.Region, verr.Error())
		httpx.WriteErr(w, httpx.Err(502, "provider_error", verr.Error()))
		return
	}
	_ = a.health.OK(r.Context(), c.TenantID, info.Module, info.Provider, info.Region, nil)

	// Cache positive results into insurance_records.
	if policy != nil && plate != "" {
		_ = a.cachePolicy(r.Context(), c.TenantID, plate, policy)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"provider": info.Provider,
		"policy":   policy,
	})
}

// POST /v1/insurance/reconcile — admin trigger; enqueues verify jobs for
// all vehicles whose insurance is expired or unknown.
func (a *API) reconcile(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT id, plate FROM vehicles
		  WHERE insurance_expires_at IS NULL OR insurance_expires_at < now()
		  LIMIT 5000`)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()
	enqueued := 0
	for rows.Next() {
		var id uuid.UUID
		var plate string
		if err := rows.Scan(&id, &plate); err != nil {
			continue
		}
		_, _ = a.queue.Enqueue(r.Context(), c.TenantID, "insurance", "verify",
			map[string]string{"vehicle_id": id.String(), "plate": plate}, 5)
		enqueued++
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"enqueued": enqueued})
}

func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	prov, _ := a.router.For(c.TenantID)
	info := prov.Info()
	state, lastOK, lastFail, streak, _ := a.health.Snapshot(r.Context(), c.TenantID, info.Module, info.Provider)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"provider":     info.Provider,
		"module":       info.Module,
		"state":        string(state),
		"fail_streak":  streak,
		"last_ok_at":   lastOK,
		"last_fail_at": lastFail,
	})
}

func (a *API) webhook(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	// Phase-2 adapters MUST verify the provider's signature here using a
	// per-provider secret stored in secret manager. The dev stub only
	// records receipt for visibility.
	a.log.Info("insurance webhook received",
		slog.String("provider", provider))
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) cachePolicy(ctx context.Context, tenant, plate string, p *insurance.Policy) error {
	conn, err := a.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return err
	}
	var vehicleID uuid.UUID
	if err := conn.QueryRow(ctx,
		`SELECT id FROM vehicles WHERE tenant_id=$1 AND plate=$2`, tenant, plate).
		Scan(&vehicleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO insurance_records
		   (tenant_id, vehicle_id, provider, policy_number, starts_at, expires_at, is_active)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		tenant, vehicleID, p.Provider, p.PolicyNumber, p.StartsAt, p.ExpiresAt, p.IsActive); err != nil {
		return err
	}
	_, err = conn.Exec(ctx,
		`UPDATE vehicles SET insurance_expires_at=$2 WHERE id=$1`,
		vehicleID, p.ExpiresAt)
	return err
}
