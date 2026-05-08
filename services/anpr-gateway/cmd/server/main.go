// ANPR gateway — Phase-1 scaffold.
//
// Accepts plate-scan events from officer devices and fixed cameras,
// records them in anpr_scans, and matches against vehicles. The actual
// computer-vision OCR runs on the edge (officer device or camera firmware);
// this gateway accepts the resulting plate string + image S3 key.
//
// Phase-2: integrate OpenALPR / PlateRecognizer / custom CV models behind
// a /v1/anpr/recognize endpoint that takes raw bytes.
package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
	"github.com/icofcucam/naditos/packages/go-common/logger"
	"github.com/icofcucam/naditos/packages/go-common/server"
)

func main() {
	cfg := config.MustLoad("anpr-gateway", 8008)
	log := logger.New(cfg.LogLevel)
	pool, err := db.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)

	mux := http.NewServeMux()
	mux.Handle("POST /v1/anpr/scans",
		issuer.Middleware(auth.RequirePermission("anpr:scan")(http.HandlerFunc(scan(pool)))))

	if err := server.Run(context.Background(), log, cfg.Port, mux); err != nil {
		log.Error("server exited", "err", err)
	}
}

func scan(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type req struct {
			Plate       string    `json:"plate"`
			Confidence  float32   `json:"confidence"`
			Source      string    `json:"source"`         // officer|fixed_cam|toll|border
			SourceID    string    `json:"source_id"`
			GeoLat      float64   `json:"geo_lat"`
			GeoLng      float64   `json:"geo_lng"`
			ImageS3Key  string    `json:"image_s3_key"`
			CapturedAt  time.Time `json:"captured_at"`
		}
		var in req
		if err := httpx.ReadJSON(r, &in); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		in.Plate = strings.ToUpper(strings.TrimSpace(in.Plate))
		if in.Plate == "" {
			httpx.WriteErr(w, httpx.Err(400, "plate_required", "plate is required"))
			return
		}
		if in.CapturedAt.IsZero() {
			in.CapturedAt = time.Now().UTC()
		}

		conn, err := db.WithTenant(r.Context(), pool)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		defer conn.Release()

		var matched *uuid.UUID
		var v uuid.UUID
		_ = conn.QueryRow(r.Context(),
			`SELECT id FROM vehicles WHERE plate=$1`, in.Plate).Scan(&v)
		if v != uuid.Nil {
			matched = &v
		}

		var id uuid.UUID
		c := auth.ClaimsFrom(r.Context())
		err = conn.QueryRow(r.Context(),
			`INSERT INTO anpr_scans
			   (tenant_id, plate_read, confidence, source, source_id,
			    captured_at, geo_lat, geo_lng, image_s3_key, matched_vehicle_id)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
			c.TenantID, in.Plate, in.Confidence, in.Source, in.SourceID,
			in.CapturedAt, in.GeoLat, in.GeoLng, in.ImageS3Key, matched).
			Scan(&id)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{
			"id":               id,
			"matched_vehicle":  matched,
		})
	}
}
