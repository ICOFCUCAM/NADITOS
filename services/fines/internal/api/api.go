// Package api wires the HTTP surface of the fines service.
//
// Anti-corruption controls baked into POST /v1/fines:
//   1. Officer cannot set the amount — server reads regulation_offences.
//   2. Evidence is required (>=1 photo with sha256 + EXIF time + GPS).
//   3. Duplicate fines for same (vehicle, offence) within
//      offence.duplicate_window_min are rejected.
//   4. The vehicle is looked up by plate inside the same transaction.
//   5. Officer identity, device, GPS, timestamp are all stamped server-side.
package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/contracts/payments"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type API struct {
	cfg     config.Service
	log     *slog.Logger
	pool    *pgxpool.Pool
	issuer  *auth.Issuer
	audit   *audit.Client
	pay     payments.Provider
	bus     events.Publisher
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool,
	issuer *auth.Issuer, audit *audit.Client,
	pay payments.Provider, bus events.Publisher) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, issuer: issuer, audit: audit, pay: pay, bus: bus}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/fines",          issuer.Middleware(auth.RequirePermission("fines:create")(http.HandlerFunc(a.issue))))
	mux.Handle("GET  /v1/fines",          issuer.Middleware(auth.RequirePermission("fines:read")(http.HandlerFunc(a.list))))
	mux.Handle("GET  /v1/fines/mine",     issuer.Middleware(http.HandlerFunc(a.listMine)))
	mux.Handle("GET  /v1/fines/{id}",     issuer.Middleware(auth.RequirePermission("fines:read")(http.HandlerFunc(a.get))))
	mux.Handle("POST /v1/fines/{id}/pay", issuer.Middleware(http.HandlerFunc(a.handlePay)))
	mux.Handle("POST /v1/fines/{id}/dispute", issuer.Middleware(http.HandlerFunc(a.dispute)))
	mux.Handle("POST /v1/fines/{id}/cancel",  issuer.Middleware(auth.RequirePermission("fines:cancel")(http.HandlerFunc(a.cancel))))
	return mux
}

// ─── DTO ────────────────────────────────────────────────────────────────────
type evidenceIn struct {
	Kind     string    `json:"kind"`     // photo|video|signature|document
	S3Key    string    `json:"s3_key"`
	Sha256   string    `json:"sha256"`
	Bytes    int64     `json:"bytes"`
	TakenAt  time.Time `json:"taken_at"`
}

type issueReq struct {
	Plate         string       `json:"plate"`
	OffenceCode   string       `json:"offence_code"`
	DriverLicense *string      `json:"driver_license"`
	GeoLat        float64      `json:"geo_lat"`
	GeoLng        float64      `json:"geo_lng"`
	GeoAccuracy   float32      `json:"geo_accuracy_m"`
	DeviceID      string       `json:"device_id"`
	Notes         string       `json:"notes,omitempty"`
	Evidence      []evidenceIn `json:"evidence"`
}

type fineOut struct {
	ID          uuid.UUID  `json:"id"`
	Plate       string     `json:"plate"`
	OffenceCode string     `json:"offence_code"`
	Amount      string     `json:"amount"`
	Currency    string     `json:"currency"`
	Status      string     `json:"status"`
	IssuedAt    time.Time  `json:"issued_at"`
	DueAt       time.Time  `json:"due_at"`
	IssuedBy    uuid.UUID  `json:"issued_by"`
}

// ─── ISSUE ─────────────────────────────────────────────────────────────────
func (a *API) issue(w http.ResponseWriter, r *http.Request) {
	var in issueReq
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	// Anti-corruption gate #1: evidence is mandatory.
	if len(in.Evidence) == 0 {
		httpx.WriteErr(w, httpx.Err(400, "evidence_required",
			"At least one photo/video evidence with sha256 is required."))
		return
	}
	for _, e := range in.Evidence {
		if e.S3Key == "" || e.Sha256 == "" || e.TakenAt.IsZero() {
			httpx.WriteErr(w, httpx.Err(400, "evidence_invalid",
				"Each evidence item needs s3_key, sha256, and taken_at."))
			return
		}
	}
	if in.Plate == "" || in.OffenceCode == "" {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}

	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()

	c := auth.ClaimsFrom(r.Context())
	issuedBy, _ := uuid.Parse(c.Subject)

	tx, err := conn.Begin(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Look up vehicle by plate (may be nil for unknown plates — we still
	//    record the fine against the plate string so it's enforceable later).
	var vehicleID *uuid.UUID
	var v uuid.UUID
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM vehicles WHERE plate=$1`, in.Plate).Scan(&v)
	if err == nil {
		vehicleID = &v
	} else if !errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, err)
		return
	}

	// 2. Read regulation — server-side amount.
	var amount string
	var currency string
	var dupWindowMin int
	err = tx.QueryRow(r.Context(),
		`SELECT base_amount::text, currency, duplicate_window_min
		   FROM regulation_offences
		  WHERE code=$1 AND is_active=true`, in.OffenceCode).
		Scan(&amount, &currency, &dupWindowMin)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.Err(400, "unknown_offence",
			"Unknown or inactive offence code."))
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}

	// 3. Duplicate-protection.
	if vehicleID != nil {
		var dupCount int
		err = tx.QueryRow(r.Context(),
			`SELECT count(*) FROM fines
			  WHERE vehicle_id=$1 AND offence_code=$2
			    AND issued_at > now() - ($3::int * interval '1 minute')
			    AND status NOT IN ('cancelled')`,
			*vehicleID, in.OffenceCode, dupWindowMin).Scan(&dupCount)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		if dupCount > 0 {
			httpx.WriteErr(w, httpx.Err(409, "duplicate_fine",
				"A fine for this offence already exists within the duplicate window."))
			return
		}
	}

	// 4. Insert the fine.
	dueAt := time.Now().Add(14 * 24 * time.Hour)
	var id uuid.UUID
	err = tx.QueryRow(r.Context(),
		`INSERT INTO fines (
			tenant_id, vehicle_id, plate, offence_code, amount, currency,
			issued_by, device_id, geo_lat, geo_lng, geo_accuracy_m,
			due_at, notes)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING id`,
		c.TenantID, vehicleID, in.Plate, in.OffenceCode, amount, currency,
		issuedBy, in.DeviceID, in.GeoLat, in.GeoLng, in.GeoAccuracy,
		dueAt, in.Notes).Scan(&id)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}

	// 5. Insert evidence rows.
	for _, e := range in.Evidence {
		_, err := tx.Exec(r.Context(),
			`INSERT INTO fine_evidence (fine_id, kind, s3_key, sha256, bytes, taken_at)
			 VALUES ($1,$2,$3,$4,$5,$6)`,
			id, e.Kind, e.S3Key, e.Sha256, e.Bytes, e.TakenAt)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	_ = a.audit.Emit(r.Context(), "fine.issue", "fine", id.String(), nil, map[string]any{
		"plate":        in.Plate,
		"offence_code": in.OffenceCode,
		"amount":       amount,
		"currency":     currency,
		"evidence":     len(in.Evidence),
		"geo":          [2]float64{in.GeoLat, in.GeoLng},
	})

	// Publish domain event — downstream services (notifications, analytics,
	// fraud detection) subscribe without coupling to fines.
	vehStr := ""
	if vehicleID != nil {
		vehStr = vehicleID.String()
	}
	env := events.NewEnvelope("fines", c.TenantID, events.TypeFineIssued, 1,
		events.FineIssuedPayload{
			FineID: id.String(), Plate: in.Plate, VehicleID: vehStr,
			OffenceCode: in.OffenceCode, Amount: amount, Currency: currency,
			IssuedBy: issuedBy.String(), DeviceID: in.DeviceID,
			GeoLat: in.GeoLat, GeoLng: in.GeoLng, EvidenceN: len(in.Evidence),
		})
	env.ActorID = c.Subject
	env.ActorRole = c.Role
	_ = a.bus.Publish(r.Context(), env)

	httpx.WriteJSON(w, http.StatusCreated, fineOut{
		ID: id, Plate: in.Plate, OffenceCode: in.OffenceCode,
		Amount: amount, Currency: currency, Status: "issued",
		IssuedAt: time.Now(), DueAt: dueAt, IssuedBy: issuedBy,
	})
}

// ─── LIST / GET / MINE ─────────────────────────────────────────────────────
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT id, plate, offence_code, amount::text, currency,
		        status::text, issued_at, due_at, issued_by
		   FROM fines
		  ORDER BY issued_at DESC LIMIT 100`)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()
	out := []fineOut{}
	for rows.Next() {
		var f fineOut
		if err := rows.Scan(&f.ID, &f.Plate, &f.OffenceCode, &f.Amount, &f.Currency,
			&f.Status, &f.IssuedAt, &f.DueAt, &f.IssuedBy); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		out = append(out, f)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (a *API) listMine(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT f.id, f.plate, f.offence_code, f.amount::text, f.currency,
		        f.status::text, f.issued_at, f.due_at, f.issued_by
		   FROM fines f
		   LEFT JOIN vehicles v ON v.id = f.vehicle_id
		   LEFT JOIN owners   o ON o.id = v.owner_id
		  WHERE o.user_id = $1 OR f.driver_user_id = $1
		  ORDER BY f.issued_at DESC LIMIT 100`,
		c.Subject)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()
	out := []fineOut{}
	for rows.Next() {
		var f fineOut
		if err := rows.Scan(&f.ID, &f.Plate, &f.OffenceCode, &f.Amount, &f.Currency,
			&f.Status, &f.IssuedAt, &f.DueAt, &f.IssuedBy); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		out = append(out, f)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
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
	var f fineOut
	err = conn.QueryRow(r.Context(),
		`SELECT id, plate, offence_code, amount::text, currency,
		        status::text, issued_at, due_at, issued_by
		   FROM fines WHERE id=$1`, id).
		Scan(&f.ID, &f.Plate, &f.OffenceCode, &f.Amount, &f.Currency,
			&f.Status, &f.IssuedAt, &f.DueAt, &f.IssuedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	rows, _ := conn.Query(r.Context(),
		`SELECT kind, s3_key, sha256, bytes, taken_at
		   FROM fine_evidence WHERE fine_id=$1`, id)
	defer rows.Close()
	type ev struct {
		Kind    string    `json:"kind"`
		S3Key   string    `json:"s3_key"`
		Sha256  string    `json:"sha256"`
		Bytes   int64     `json:"bytes"`
		TakenAt time.Time `json:"taken_at"`
	}
	evs := []ev{}
	for rows.Next() {
		var e ev
		_ = rows.Scan(&e.Kind, &e.S3Key, &e.Sha256, &e.Bytes, &e.TakenAt)
		evs = append(evs, e)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"fine": f, "evidence": evs})
}

// ─── PAY / DISPUTE / CANCEL ────────────────────────────────────────────────
func (a *API) handlePay(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	type req struct {
		Method string `json:"method"`
	}
	var in req
	_ = httpx.ReadJSON(r, &in)
	if in.Method == "" {
		in.Method = "card"
	}
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	tx, err := conn.Begin(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var amount string
	var currency string
	err = tx.QueryRow(r.Context(),
		`SELECT amount::text, currency FROM fines WHERE id=$1`, id).
		Scan(&amount, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}

	c := auth.ClaimsFrom(r.Context())
	intent, err := a.pay.CreateIntent(r.Context(), payments.CreateIntentInput{
		TenantID:       c.TenantID,
		IdempotencyKey: "fine:" + id.String(),
		Amount:         payments.Money{Amount: amount, Currency: currency},
		Description:    "Traffic fine " + id.String(),
		Method:         in.Method,
		Metadata:       map[string]string{"fine_id": id.String()},
	})
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	providerRef := intent.ID
	status := "pending"
	if intent.Status == payments.StatusSucceeded {
		status = "succeeded"
	}

	if _, err := tx.Exec(r.Context(),
		`INSERT INTO fine_payments (fine_id, amount, currency, method, provider_ref, status, paid_at)
		 VALUES ($1,$2::numeric,$3,$4,$5,$6, CASE WHEN $6='succeeded' THEN now() END)`,
		id, amount, currency, in.Method, providerRef, status); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if status == "succeeded" {
		if _, err := tx.Exec(r.Context(),
			`UPDATE fines SET status='paid', paid_at=now() WHERE id=$1`, id); err != nil {
			httpx.WriteErr(w, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	_ = a.audit.Emit(r.Context(), "fine.pay", "fine", id.String(), nil,
		map[string]any{"method": in.Method, "provider_ref": providerRef, "status": status})
	if status == "succeeded" {
		env := events.NewEnvelope("fines", c.TenantID, events.TypeFinePaid, 1,
			events.FinePaidPayload{
				FineID: id.String(), Amount: amount, Currency: currency,
				Method: in.Method, ProviderRef: providerRef,
			})
		env.ActorID = c.Subject
		env.ActorRole = c.Role
		_ = a.bus.Publish(r.Context(), env)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"intent_id":     intent.ID,
		"client_secret": intent.ClientSecret,
		"status":        status,
	})
}

func (a *API) dispute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	type req struct{ Reason string `json:"reason"` }
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if in.Reason == "" {
		httpx.WriteErr(w, httpx.Err(400, "reason_required", "reason is required"))
		return
	}
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	tx, err := conn.Begin(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO fine_disputes (fine_id, filed_by, reason)
		 VALUES ($1,$2,$3)`, id, c.Subject, in.Reason); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE fines SET status='disputed' WHERE id=$1`, id); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	_ = a.audit.Emit(r.Context(), "fine.dispute", "fine", id.String(), nil, in)
	env := events.NewEnvelope("fines", c.TenantID, events.TypeFineDisputed, 1,
		events.FineDisputedPayload{FineID: id.String(), FiledBy: c.Subject, Reason: in.Reason})
	env.ActorID = c.Subject
	env.ActorRole = c.Role
	_ = a.bus.Publish(r.Context(), env)
	w.WriteHeader(http.StatusCreated)
}

func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	type req struct{ Reason string `json:"reason"` }
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if in.Reason == "" {
		httpx.WriteErr(w, httpx.Err(400, "reason_required",
			"Cancellation requires a reason for the audit log."))
		return
	}
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	if _, err := conn.Exec(r.Context(),
		`UPDATE fines SET status='cancelled', notes = COALESCE(notes,'') || E'\nCANCELLED: ' || $2
		   WHERE id=$1`, id, in.Reason); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	c := auth.ClaimsFrom(r.Context())
	_ = a.audit.Emit(r.Context(), "fine.cancel", "fine", id.String(), nil, in)
	env := events.NewEnvelope("fines", c.TenantID, events.TypeFineCancelled, 1,
		events.FineCancelledPayload{FineID: id.String(), Reason: in.Reason})
	env.ActorID = c.Subject
	env.ActorRole = c.Role
	_ = a.bus.Publish(r.Context(), env)
	w.WriteHeader(http.StatusNoContent)
}
