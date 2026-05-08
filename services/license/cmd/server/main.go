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
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
)

type license struct {
	ID            uuid.UUID `json:"id"`
	UserID        *uuid.UUID `json:"user_id,omitempty"`
	LicenseNumber string    `json:"license_number"`
	FullName      string    `json:"full_name"`
	Classes       []string  `json:"classes"`
	IssuedAt      *string   `json:"issued_at,omitempty"`
	ExpiresAt     *string   `json:"expires_at,omitempty"`
	Points        int       `json:"points"`
	IsSuspended   bool      `json:"is_suspended"`
}

func main() {
	cfg := config.MustLoad("license", 8003)
	log := logger.New(cfg.LogLevel)
	pool, err := db.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/licenses/by-number/{n}",
		issuer.Middleware(auth.RequirePermission("license:read")(http.HandlerFunc(byNumber(pool, log)))))
	mux.Handle("POST /v1/licenses",
		issuer.Middleware(auth.RequirePermission("license:write")(http.HandlerFunc(create(pool, log)))))

	if err := server.Run(context.Background(), log, cfg.Port, mux); err != nil {
		log.Error("server exited", "err", err)
	}
}

func byNumber(pool *pgxpool.Pool, _ *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := db.WithTenant(r.Context(), pool)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		defer conn.Release()
		var l license
		err = conn.QueryRow(r.Context(),
			`SELECT id, user_id, license_number, full_name, classes,
			        to_char(issued_at,'YYYY-MM-DD'),
			        to_char(expires_at,'YYYY-MM-DD'),
			        points, is_suspended
			   FROM driver_licenses WHERE license_number=$1`, r.PathValue("n")).
			Scan(&l.ID, &l.UserID, &l.LicenseNumber, &l.FullName, &l.Classes,
				&l.IssuedAt, &l.ExpiresAt, &l.Points, &l.IsSuspended)
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteErr(w, httpx.ErrNotFound)
			return
		}
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, l)
	}
}

func create(pool *pgxpool.Pool, _ *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type req struct {
			LicenseNumber string   `json:"license_number"`
			FullName      string   `json:"full_name"`
			Classes       []string `json:"classes"`
			IssuedAt      *string  `json:"issued_at"`
			ExpiresAt     *string  `json:"expires_at"`
		}
		var in req
		if err := httpx.ReadJSON(r, &in); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		c := auth.ClaimsFrom(r.Context())
		conn, err := db.WithTenant(r.Context(), pool)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		defer conn.Release()
		var id uuid.UUID
		err = conn.QueryRow(r.Context(),
			`INSERT INTO driver_licenses
			   (tenant_id, license_number, full_name, classes, issued_at, expires_at)
			 VALUES ($1,$2,$3,$4,$5::date,$6::date) RETURNING id`,
			c.TenantID, in.LicenseNumber, in.FullName, in.Classes, in.IssuedAt, in.ExpiresAt).
			Scan(&id)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
	}
}
