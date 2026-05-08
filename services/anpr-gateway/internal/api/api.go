// Package api wires the ANPR gateway's HTTP surface.
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts/anpr"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type API struct {
	cfg        config.Service
	log        *slog.Logger
	pool       *pgxpool.Pool
	issuer     *auth.Issuer
	recognizer anpr.Recognizer
	hm         *connectors.HealthMonitor
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool,
	issuer *auth.Issuer, recognizer anpr.Recognizer,
	health *connectors.HealthMonitor) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, issuer: issuer,
		recognizer: recognizer, hm: health}
	mux := http.NewServeMux()

	// Single-scan ingest. Officer devices and edge cameras hit this in
	// real time. The handler ENQUEUES — it never blocks on matching.
	mux.Handle("POST /v1/anpr/scans",
		issuer.Middleware(auth.RequirePermission("anpr:scan")(http.HandlerFunc(a.enqueue))))

	// Batch ingest for offline reconciliation: a police PWA that lost
	// connectivity replays its queue when it comes back online.
	mux.Handle("POST /v1/anpr/scans:batch",
		issuer.Middleware(auth.RequirePermission("anpr:scan")(http.HandlerFunc(a.enqueueBatch))))

	// Recognize: synchronous OCR on a freshly captured image. Officer
	// uploads a photo, gets back ranked plate candidates, then submits
	// the chosen one to /v1/anpr/scans for async matching.
	mux.Handle("POST /v1/anpr/recognize",
		issuer.Middleware(auth.RequirePermission("anpr:scan")(http.HandlerFunc(a.recognize))))

	// Job status — clients poll for the resolved scan + match.
	mux.Handle("GET /v1/anpr/jobs/{id}",
		issuer.Middleware(auth.RequirePermission("anpr:scan")(http.HandlerFunc(a.jobStatus))))

	// Recent scans — for officer feed.
	mux.Handle("GET /v1/anpr/scans",
		issuer.Middleware(auth.RequirePermission("anpr:scan")(http.HandlerFunc(a.listScans))))

	// Provider health snapshot — what's wired right now.
	mux.Handle("GET /v1/anpr/health",
		issuer.Middleware(http.HandlerFunc(a.health)))

	return mux
}

// ─── DTO ────────────────────────────────────────────────────────────────────
type scanIn struct {
	Plate      string    `json:"plate"`
	Confidence float32   `json:"confidence"`
	Source     string    `json:"source"`         // officer|fixed_cam|toll|border|highway
	SourceID   string    `json:"source_id"`
	GeoLat     float64   `json:"geo_lat"`
	GeoLng     float64   `json:"geo_lng"`
	ImageS3Key string    `json:"image_s3_key"`
	CapturedAt time.Time `json:"captured_at"`
}

// ─── Handlers ───────────────────────────────────────────────────────────────
func (a *API) enqueue(w http.ResponseWriter, r *http.Request) {
	var in scanIn
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	id, err := a.enqueueOne(r.Context(), in)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"job_id": id, "status": "queued",
	})
}

func (a *API) enqueueBatch(w http.ResponseWriter, r *http.Request) {
	type req struct{ Items []scanIn `json:"items"` }
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if len(in.Items) == 0 || len(in.Items) > 1000 {
		httpx.WriteErr(w, httpx.Err(400, "batch_size", "items must be 1..1000"))
		return
	}
	ids := make([]uuid.UUID, 0, len(in.Items))
	for _, it := range in.Items {
		id, err := a.enqueueOne(r.Context(), it)
		if err != nil {
			a.log.Warn("anpr batch item failed", "err", err)
			continue
		}
		ids = append(ids, id)
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"accepted": len(ids), "job_ids": ids,
	})
}

func (a *API) enqueueOne(ctx context.Context, in scanIn) (uuid.UUID, error) {
	c := auth.ClaimsFrom(ctx)
	if c == nil {
		return uuid.Nil, httpx.ErrUnauthorized
	}
	if in.Plate == "" {
		return uuid.Nil, httpx.Err(400, "plate_required", "plate is required")
	}
	if in.CapturedAt.IsZero() {
		in.CapturedAt = time.Now().UTC()
	}
	conn, err := db.WithTenant(ctx, a.pool)
	if err != nil {
		return uuid.Nil, err
	}
	defer conn.Release()
	var id uuid.UUID
	err = conn.QueryRow(ctx,
		`INSERT INTO anpr_jobs
		   (tenant_id, source, source_id, raw_plate, confidence,
		    geo_lat, geo_lng, image_s3_key, captured_at)
		 VALUES ($1,$2, NULLIF($3,''),$4,$5,$6,$7, NULLIF($8,''),$9)
		 RETURNING id`,
		c.TenantID, in.Source, in.SourceID, in.Plate, in.Confidence,
		nilIfZero(in.GeoLat), nilIfZero(in.GeoLng), in.ImageS3Key, in.CapturedAt).
		Scan(&id)
	return id, err
}

func (a *API) jobStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()

	type out struct {
		ID               uuid.UUID  `json:"id"`
		Status           string     `json:"status"`
		NormalizedPlate  *string    `json:"normalized_plate,omitempty"`
		Confidence       float32    `json:"confidence"`
		Attempts         int        `json:"attempts"`
		LastError        *string    `json:"last_error,omitempty"`
		ScanID           *uuid.UUID `json:"scan_id,omitempty"`
		MatchedVehicleID *uuid.UUID `json:"matched_vehicle_id,omitempty"`
		EnqueuedAt       time.Time  `json:"enqueued_at"`
		ProcessedAt      *time.Time `json:"processed_at,omitempty"`
	}
	var o out
	err = conn.QueryRow(r.Context(),
		`SELECT j.id, j.status::text, j.normalized_plate, j.confidence, j.attempts,
		        j.last_error, j.scan_id, s.matched_vehicle_id, j.enqueued_at, j.processed_at
		   FROM anpr_jobs j
		   LEFT JOIN anpr_scans s ON s.id = j.scan_id
		  WHERE j.id=$1`, id).
		Scan(&o.ID, &o.Status, &o.NormalizedPlate, &o.Confidence, &o.Attempts,
			&o.LastError, &o.ScanID, &o.MatchedVehicleID, &o.EnqueuedAt, &o.ProcessedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (a *API) listScans(w http.ResponseWriter, r *http.Request) {
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT id, plate_read, confidence, source, captured_at,
		        geo_lat, geo_lng, matched_vehicle_id
		   FROM anpr_scans ORDER BY captured_at DESC LIMIT 100`)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()
	type row struct {
		ID               uuid.UUID  `json:"id"`
		Plate            string     `json:"plate"`
		Confidence       float32    `json:"confidence"`
		Source           string     `json:"source"`
		CapturedAt       time.Time  `json:"captured_at"`
		GeoLat           *float64   `json:"geo_lat"`
		GeoLng           *float64   `json:"geo_lng"`
		MatchedVehicleID *uuid.UUID `json:"matched_vehicle_id"`
	}
	out := []row{}
	for rows.Next() {
		var it row
		_ = rows.Scan(&it.ID, &it.Plate, &it.Confidence, &it.Source, &it.CapturedAt,
			&it.GeoLat, &it.GeoLng, &it.MatchedVehicleID)
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func nilIfZero(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

// recognize accepts a multipart upload of one image and runs the
// configured Recognizer synchronously. Returns ranked candidates so
// the officer's PWA can show "we read it as ABC123 (94%)" and let the
// officer confirm or override. The chosen plate is then submitted
// separately to POST /v1/anpr/scans for async matching.
//
//	multipart/form-data:
//	  image     (required)  the captured frame, jpeg/png
//	  country   (optional)  2-letter override (e.g. "fr"); defaults to
//	                         provider config
//	  min_conf  (optional)  0..1 cutoff; reads below are dropped
const recognizeMaxBytes = 8 << 20 // 8 MiB

func (a *API) recognize(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	if err := r.ParseMultipartForm(recognizeMaxBytes); err != nil {
		httpx.WriteErr(w, httpx.Err(http.StatusBadRequest, "bad_multipart", err.Error()))
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		httpx.WriteErr(w, httpx.Err(http.StatusBadRequest, "missing_image", "form field 'image' is required"))
		return
	}
	defer file.Close()

	// Cap at 8 MiB so a malicious upload can't OOM the worker.
	body, err := io.ReadAll(io.LimitReader(file, recognizeMaxBytes))
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if len(body) == 0 {
		httpx.WriteErr(w, httpx.Err(http.StatusBadRequest, "empty_image", "image is empty"))
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	opts := anpr.RecognizeOpts{TenantID: c.TenantID, Country: r.FormValue("country")}
	if v := r.FormValue("min_conf"); v != "" {
		var f float32
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			opts.MinConf = f
		}
	}

	reads, err := a.recognizer.Recognize(r.Context(),
		anpr.Image{Bytes: body, ContentType: contentType, CapturedAt: time.Now().UTC()},
		opts)
	info := a.recognizer.Info()
	if err != nil {
		// 502 because the failure is from the upstream provider. Also
		// stamp the failure into the per-tenant HealthMonitor so the
		// /providers admin dashboard surfaces the streak.
		if a.hm != nil {
			_ = a.hm.Fail(r.Context(), c.TenantID, info.Module, info.Provider, info.Region, err.Error())
		}
		a.log.Warn("anpr recognize failed",
			slog.String("provider", info.Provider),
			slog.String("err", err.Error()))
		httpx.WriteErr(w, httpx.Err(http.StatusBadGateway, "recognize_failed", err.Error()))
		return
	}
	if a.hm != nil {
		_ = a.hm.OK(r.Context(), c.TenantID, info.Module, info.Provider, info.Region, nil)
	}

	type readOut struct {
		Plate      string  `json:"plate"`
		Confidence float32 `json:"confidence"`
		Region     string  `json:"region,omitempty"`
		BBox       *anpr.BBox `json:"bbox,omitempty"`
	}
	out := make([]readOut, 0, len(reads))
	for _, rd := range reads {
		out = append(out, readOut{Plate: rd.Plate, Confidence: rd.Confidence, Region: rd.Region, BBox: rd.BBox})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"provider": a.recognizer.Info().Provider,
		"reads":    out,
	})
}

// health reports which ANPR provider is currently bound to this replica.
// The payload mirrors the shape /v1/insurance/health uses so a single
// admin "providers" page can render all of them uniformly.
//
// state / fail_streak / last_ok / last_fail come from the
// per-(tenant,module,provider) HealthMonitor; if no recognize call has
// been recorded yet the snapshot is empty and we omit those fields.
func (a *API) health(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	info := a.recognizer.Info()
	resp := map[string]any{
		"module":   info.Module,
		"provider": info.Provider,
		"region":   info.Region,
	}
	if a.hm != nil && c != nil {
		state, lastOK, lastFail, streak, err := a.hm.Snapshot(
			r.Context(), c.TenantID, info.Module, info.Provider)
		if err == nil {
			resp["state"] = string(state)
			resp["fail_streak"] = streak
			resp["last_ok_at"] = nullableTime(lastOK)
			resp["last_fail_at"] = nullableTime(lastFail)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
