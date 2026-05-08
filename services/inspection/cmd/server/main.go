// Roadworthiness / EU inspection service — Phase-2.
//
// Mirrors the insurance service: per-tenant provider router, retry queue,
// health monitor, online verify endpoint, webhook receiver, reconcile job.
package main

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
	"github.com/icofcucam/naditos/packages/go-common/contracts/inspection"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
	"github.com/icofcucam/naditos/services/inspection/internal/worker"
)

func main() {
	cfg := config.MustLoad("inspection", 8005)
	log := logger.New(cfg.LogLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db open failed", "err", err)
		panic(err)
	}
	defer pool.Close()

	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)

	router := connectors.NewRouter[inspection.Verifier]()
	router.SetDefault(inspection.NewDevStub())
	health := connectors.NewHealthMonitor(pool)
	queue := connectors.NewRetryQueue(pool)

	// Background worker drains the verify retry queue. Mirrors the
	// shape insurance uses; Phase-4 may extract a generic drainer.
	go worker.New(pool, log, router, health, queue).Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/inspection/verify",
		issuer.Middleware(http.HandlerFunc(verifyHandler(pool, log, router, health))))
	mux.Handle("POST /v1/inspection/reconcile",
		issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(reconcileHandler(pool, queue)))))
	mux.Handle("GET /v1/inspection/health",
		issuer.Middleware(http.HandlerFunc(healthHandler(router, health))))
	mux.HandleFunc("POST /v1/inspection/webhooks/{provider}", webhookHandler(log))

	if err := server.Run(ctx, log, "inspection", cfg.Port, mux); err != nil {
		log.Error("server exited", "err", err)
	}
}

func verifyHandler(pool *pgxpool.Pool, log *slog.Logger,
	router *connectors.CountryRouter[inspection.Verifier],
	health *connectors.HealthMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := auth.ClaimsFrom(r.Context())
		plate := r.URL.Query().Get("plate")
		if plate == "" {
			httpx.WriteErr(w, httpx.Err(400, "missing", "plate required"))
			return
		}
		prov, err := router.For(c.TenantID)
		if err != nil {
			httpx.WriteErr(w, httpx.Err(503, "no_provider", err.Error()))
			return
		}
		rec, vErr := prov.VerifyByPlate(r.Context(), c.TenantID, plate)
		info := prov.Info()
		if vErr != nil {
			_ = health.Fail(r.Context(), c.TenantID, info.Module, info.Provider, info.Region, vErr.Error())
			httpx.WriteErr(w, httpx.Err(502, "provider_error", vErr.Error()))
			return
		}
		_ = health.OK(r.Context(), c.TenantID, info.Module, info.Provider, info.Region, nil)
		if rec != nil {
			_ = cacheRecord(r.Context(), pool, c.TenantID, plate, rec)
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"provider": info.Provider,
			"record":   rec,
		})
	}
}

func reconcileHandler(pool *pgxpool.Pool, queue *connectors.RetryQueue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := auth.ClaimsFrom(r.Context())
		conn, err := db.WithTenant(r.Context(), pool)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		defer conn.Release()
		rows, err := conn.Query(r.Context(),
			`SELECT id, plate FROM vehicles
			  WHERE inspection_expires_at IS NULL OR inspection_expires_at < now()
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
			_, _ = queue.Enqueue(r.Context(), c.TenantID, "inspection", "verify",
				map[string]string{"vehicle_id": id.String(), "plate": plate}, 5)
			enqueued++
		}
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"enqueued": enqueued})
	}
}

func healthHandler(router *connectors.CountryRouter[inspection.Verifier],
	health *connectors.HealthMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := auth.ClaimsFrom(r.Context())
		prov, _ := router.For(c.TenantID)
		info := prov.Info()
		state, lastOK, lastFail, streak, _ := health.Snapshot(r.Context(), c.TenantID, info.Module, info.Provider)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"provider":     info.Provider,
			"module":       info.Module,
			"state":        string(state),
			"fail_streak":  streak,
			"last_ok_at":   lastOK,
			"last_fail_at": lastFail,
		})
	}
}

func webhookHandler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Info("inspection webhook received",
			slog.String("provider", r.PathValue("provider")))
		w.WriteHeader(http.StatusAccepted)
	}
}

func cacheRecord(ctx context.Context, pool *pgxpool.Pool, tenant, plate string, rec *inspection.Record) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return err
	}
	var vid uuid.UUID
	if err := conn.QueryRow(ctx,
		`SELECT id FROM vehicles WHERE tenant_id=$1 AND plate=$2`, tenant, plate).
		Scan(&vid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO inspection_records
		   (tenant_id, vehicle_id, station, performed_at, expires_at, result, certificate_url)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		tenant, vid, rec.Station, rec.PerformedAt, rec.ExpiresAt, rec.Result, rec.CertificateURL); err != nil {
		return err
	}
	_, err = conn.Exec(ctx,
		`UPDATE vehicles SET inspection_expires_at=$2 WHERE id=$1`,
		vid, rec.ExpiresAt)
	return err
}
