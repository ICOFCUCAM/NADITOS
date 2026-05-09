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
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts/payments"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

// Reaper is the small subset of reaper.Job the api package depends
// on — kept as an interface so the api package doesn't pull the full
// reaper surface (and its storage import) into tests that don't need
// it. Tests can pass nil to disable the trigger endpoint.
type Reaper interface {
	RunOnce(ctx context.Context) (int, error)
}

type API struct {
	cfg       config.Service
	log       *slog.Logger
	pool      *pgxpool.Pool
	adminPool *pgxpool.Pool
	issuer    *auth.Issuer
	audit     *audit.Client
	pay       payments.Provider
	hm        *connectors.HealthMonitor
	bus       events.Publisher
	reap      Reaper
}

// New wires the fines API.
//
// adminPool is a pgxpool.Pool whose underlying DB role bypasses RLS;
// it's only used by the payment webhook handler, which has no JWT and
// therefore can't satisfy the per-tenant policies the request handlers
// rely on. In production, callers may pass the same pool twice if the
// runtime DB user already has BYPASSRLS — tests pass a separate
// BYPASSRLS pool.
func New(cfg config.Service, log *slog.Logger, pool, adminPool *pgxpool.Pool,
	issuer *auth.Issuer, audit *audit.Client,
	pay payments.Provider, hm *connectors.HealthMonitor, bus events.Publisher,
	reap Reaper) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, adminPool: adminPool,
		issuer: issuer, audit: audit, pay: pay, hm: hm, bus: bus, reap: reap}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/fines",          issuer.Middleware(auth.RequirePermission("fines:create")(http.HandlerFunc(a.issue))))
	mux.Handle("GET  /v1/fines",          issuer.Middleware(auth.RequirePermission("fines:read")(http.HandlerFunc(a.list))))
	mux.Handle("GET  /v1/fines/mine",     issuer.Middleware(http.HandlerFunc(a.listMine)))
	mux.Handle("GET  /v1/fines/{id}",     issuer.Middleware(auth.RequirePermission("fines:read")(http.HandlerFunc(a.get))))
	mux.Handle("POST /v1/fines/{id}/pay", issuer.Middleware(http.HandlerFunc(a.handlePay)))
	mux.Handle("POST /v1/fines/{id}/dispute", issuer.Middleware(http.HandlerFunc(a.dispute)))
	mux.Handle("POST /v1/fines/{id}/cancel",  issuer.Middleware(auth.RequirePermission("fines:cancel")(http.HandlerFunc(a.cancel))))
	mux.Handle("GET  /v1/fines/payments/health",
		issuer.Middleware(http.HandlerFunc(a.paymentsHealth)))
	// Admin-only synchronous reaper trigger. The background sweep runs
	// every 6h; ops staff need a way to force a sweep without restarting
	// the service (e.g. after backfilling a tenant's retention policy).
	// Reuses the existing fines:cancel permission — admins have it,
	// nobody else does. Reaping evidence is in the same destructive-
	// admin tier.
	mux.Handle("POST /v1/fines/admin/reaper:run",
		issuer.Middleware(auth.RequirePermission("fines:cancel")(http.HandlerFunc(a.runReaper))))
	// Provider webhooks are unauthenticated by client JWT — the proof is
	// the signature on the body. RequirePermission would deny them
	// outright, so this route deliberately bypasses issuer.Middleware.
	// The path is namespaced under /payments/ to avoid colliding with
	// POST /v1/fines/{id}/pay (which would otherwise match
	// /v1/fines/webhooks/X with {id}="webhooks").
	mux.Handle("POST /v1/fines/payments/webhooks/{provider}",
		http.HandlerFunc(a.handleWebhook))
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
	ID              uuid.UUID `json:"id"`
	Plate           string    `json:"plate"`
	OffenceCode     string    `json:"offence_code"`
	Amount          string    `json:"amount"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	IssuedAt        time.Time `json:"issued_at"`
	DueAt           time.Time `json:"due_at"`
	IssuedBy        uuid.UUID `json:"issued_by"`
	EscalationStage int       `json:"escalation_stage"`
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

	// 4. Optional driver-license linkage. When the officer scans the
	//    citizen's QR/NFC bundle (or types their license number) the
	//    fine is attached to the license so the demerit engine can
	//    apply points. Lookup is tenant-scoped; an unknown number is
	//    a 400 — the officer should not be guessing.
	var driverLicenseID *uuid.UUID
	if in.DriverLicense != nil && *in.DriverLicense != "" {
		var lid uuid.UUID
		err := tx.QueryRow(r.Context(),
			`SELECT id FROM driver_licenses WHERE license_number=$1`, *in.DriverLicense).Scan(&lid)
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteErr(w, httpx.Err(400, "unknown_license",
				"Driver license number not found."))
			return
		}
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		driverLicenseID = &lid
	}

	// 5. Insert the fine.
	dueAt := time.Now().Add(14 * 24 * time.Hour)
	var id uuid.UUID
	err = tx.QueryRow(r.Context(),
		`INSERT INTO fines (
			tenant_id, vehicle_id, plate, offence_code, amount, currency,
			issued_by, device_id, geo_lat, geo_lng, geo_accuracy_m,
			due_at, notes, driver_license_id)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 RETURNING id`,
		c.TenantID, vehicleID, in.Plate, in.OffenceCode, amount, currency,
		issuedBy, in.DeviceID, in.GeoLat, in.GeoLng, in.GeoAccuracy,
		dueAt, in.Notes, driverLicenseID).Scan(&id)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}

	// 5. Insert evidence rows AND a chain-of-custody anchor row per
	//    evidence item. The custody table is append-only by convention;
	//    every subsequent action on an evidence object (verified,
	//    viewed, exported, sealed) appends another row pointing at the
	//    same evidence_id. The first 'captured' entry is what proves
	//    the evidence existed at issuance time.
	for _, e := range in.Evidence {
		var evidenceID uuid.UUID
		if err := tx.QueryRow(r.Context(),
			`INSERT INTO fine_evidence (fine_id, kind, s3_key, sha256, bytes, taken_at)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 RETURNING id`,
			id, e.Kind, e.S3Key, e.Sha256, e.Bytes, e.TakenAt).Scan(&evidenceID); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO evidence_custody
			   (tenant_id, fine_id, evidence_id, action,
			    actor_user, actor_role, actor_device, details)
			 VALUES ($1, $2, $3, 'captured',
			         $4, $5, $6,
			         jsonb_build_object('sha256', $7::text, 'kind', $8::text))`,
			c.TenantID, id, evidenceID,
			issuedBy, c.Role, in.DeviceID, e.Sha256, e.Kind); err != nil {
			httpx.WriteErr(w, err)
			return
		}
	}

	// 6. Write the domain event into the outbox INSIDE the same tx. If
	//    we crash between Commit and a direct bus.Publish, the relay
	//    picks it up; the producer never has to think about delivery.
	vehStr := ""
	if vehicleID != nil {
		vehStr = vehicleID.String()
	}
	env := events.EnvelopeFromContext(r.Context(), "fines", c.TenantID, events.TypeFineIssued, 1,
		events.FineIssuedPayload{
			FineID: id.String(), Plate: in.Plate, VehicleID: vehStr,
			OffenceCode: in.OffenceCode, Amount: amount, Currency: currency,
			IssuedBy: issuedBy.String(), DeviceID: in.DeviceID,
			GeoLat: in.GeoLat, GeoLng: in.GeoLng, EvidenceN: len(in.Evidence),
		})
	if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	// Audit goes over HTTP and is best-effort; the in-tx outbox above is
	// the durable record. If audit emit fails, the event still survives.
	_ = a.audit.Emit(r.Context(), "fine.issue", "fine", id.String(), nil, map[string]any{
		"plate":        in.Plate,
		"offence_code": in.OffenceCode,
		"amount":       amount,
		"currency":     currency,
		"evidence":     len(in.Evidence),
		"geo":          [2]float64{in.GeoLat, in.GeoLng},
	})

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
		        status::text, issued_at, due_at, issued_by, escalation_stage
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
			&f.Status, &f.IssuedAt, &f.DueAt, &f.IssuedBy, &f.EscalationStage); err != nil {
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
		        f.status::text, f.issued_at, f.due_at, f.issued_by,
		        f.escalation_stage
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
			&f.Status, &f.IssuedAt, &f.DueAt, &f.IssuedBy, &f.EscalationStage); err != nil {
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
		        status::text, issued_at, due_at, issued_by, escalation_stage
		   FROM fines WHERE id=$1`, id).
		Scan(&f.ID, &f.Plate, &f.OffenceCode, &f.Amount, &f.Currency,
			&f.Status, &f.IssuedAt, &f.DueAt, &f.IssuedBy, &f.EscalationStage)
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

	// Chain of custody — every action ever taken on every evidence
	// item belonging to this fine, in chronological order. The court
	// or defense reads this to reconstruct who did what and when.
	custodyRows, err := conn.Query(r.Context(),
		`SELECT cu.evidence_id, cu.action, cu.actor_user, cu.actor_role,
		        cu.actor_device, cu.details, cu.occurred_at
		   FROM evidence_custody cu
		  WHERE cu.fine_id = $1
		  ORDER BY cu.occurred_at ASC, cu.id ASC`, id)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer custodyRows.Close()
	type custody struct {
		EvidenceID  uuid.UUID  `json:"evidence_id"`
		Action      string     `json:"action"`
		ActorUser   *uuid.UUID `json:"actor_user"`
		ActorRole   *string    `json:"actor_role"`
		ActorDevice *string    `json:"actor_device"`
		Details     any        `json:"details"`
		OccurredAt  time.Time  `json:"occurred_at"`
	}
	custodyList := []custody{}
	for custodyRows.Next() {
		var co custody
		var details *string
		_ = custodyRows.Scan(&co.EvidenceID, &co.Action, &co.ActorUser,
			&co.ActorRole, &co.ActorDevice, &details, &co.OccurredAt)
		if details != nil {
			co.Details = *details
		}
		custodyList = append(custodyList, co)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"fine":     f,
		"evidence": evs,
		"custody":  custodyList,
	})
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
	info := a.pay.Info()
	if err != nil {
		if a.hm != nil {
			_ = a.hm.Fail(r.Context(), c.TenantID,
				info.Module, info.Provider, info.Region, err.Error())
		}
		httpx.WriteErr(w, err)
		return
	}
	if a.hm != nil {
		_ = a.hm.OK(r.Context(), c.TenantID,
			info.Module, info.Provider, info.Region, nil)
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
		// Outbox the event in the same tx — successful payment + event
		// publish are now atomic.
		env := events.EnvelopeFromContext(r.Context(), "fines", c.TenantID, events.TypeFinePaid, 1,
			events.FinePaidPayload{
				FineID: id.String(), Amount: amount, Currency: currency,
				Method: in.Method, ProviderRef: providerRef,
			})
		if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
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

	// Ownership + status gate. Citizens can only dispute fines they own
	// (vehicle owner OR listed driver) and only while the fine is still
	// open. Admin/court staff disputing on a citizen's behalf is
	// out-of-scope — that path goes through fine_disputes directly.
	var status, ownerOK string
	err = tx.QueryRow(r.Context(),
		`SELECT f.status::text,
		        CASE WHEN o.user_id = $2 OR f.driver_user_id = $2
		             THEN 'yes' ELSE 'no' END
		   FROM fines f
		   LEFT JOIN vehicles v ON v.id = f.vehicle_id
		   LEFT JOIN owners   o ON o.id = v.owner_id
		  WHERE f.id = $1`, id, c.Subject).Scan(&status, &ownerOK)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if ownerOK != "yes" {
		httpx.WriteErr(w, httpx.ErrForbidden)
		return
	}
	if status == "paid" || status == "cancelled" || status == "disputed" {
		httpx.WriteErr(w, httpx.Err(http.StatusConflict, "not_disputable",
			"fine is "+status+"; can no longer be disputed"))
		return
	}

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
	env := events.EnvelopeFromContext(r.Context(), "fines", c.TenantID, events.TypeFineDisputed, 1,
		events.FineDisputedPayload{FineID: id.String(), FiledBy: c.Subject, Reason: in.Reason})
	if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	_ = a.audit.Emit(r.Context(), "fine.dispute", "fine", id.String(), nil, in)
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
	tx, err := conn.Begin(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`UPDATE fines SET status='cancelled', notes = COALESCE(notes,'') || E'\nCANCELLED: ' || $2
		   WHERE id=$1`, id, in.Reason); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	c := auth.ClaimsFrom(r.Context())
	env := events.EnvelopeFromContext(r.Context(), "fines", c.TenantID, events.TypeFineCancelled, 1,
		events.FineCancelledPayload{FineID: id.String(), Reason: in.Reason})
	if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	_ = a.audit.Emit(r.Context(), "fine.cancel", "fine", id.String(), nil, in)
	w.WriteHeader(http.StatusNoContent)
}

// handleWebhook accepts a provider-signed payment notification, verifies
// the signature via the bound payments.Provider, and atomically marks
// the matching fine paid + writes a fine.paid event to the outbox.
//
// Authentication is by signature, not JWT. {provider} in the URL must
// match the bound provider's identity — anything else is rejected
// before we even read the body.
//
// Idempotency: the same intent ID arriving twice is a no-op. Real
// providers retry aggressively, so this matters in production.
//
// Tenant: the fine's tenant_id is read out of fine_payments → fines;
// the webhook itself has no JWT, so we trust the row we look up.
func (a *API) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if got := r.PathValue("provider"); got != a.pay.Info().Provider {
		httpx.WriteErr(w, httpx.Err(http.StatusNotFound, "unknown_provider",
			"no payments provider bound for "+got))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.WriteErr(w, httpx.Err(http.StatusBadRequest, "read_failed", err.Error()))
		return
	}
	headers := map[string]string{}
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}

	evt, err := a.pay.VerifyWebhook(r.Context(), headers, body)
	if err != nil {
		// Treat any verify failure as a signature problem (401). The
		// provider will retry; logging the upstream payload would leak
		// secrets, so just stamp the failure on the health monitor.
		info := a.pay.Info()
		if a.hm != nil {
			_ = a.hm.Fail(r.Context(), "", info.Module, info.Provider, info.Region, err.Error())
		}
		httpx.WriteErr(w, httpx.Err(http.StatusUnauthorized, "signature_invalid", err.Error()))
		return
	}

	// Only "succeeded" transitions the fine. Other statuses (processing,
	// failed, refunded) are recorded against the fine_payments row but
	// don't mutate fines.status here — refund flows live elsewhere.
	if evt.Status != payments.StatusSucceeded {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Webhooks have no JWT and therefore no app_tenant — handler runs
	// against the admin pool (BYPASSRLS) so RLS doesn't hide the
	// fine_payments row we need to look up by provider_ref.
	conn, err := a.adminPool.Acquire(r.Context())
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

	var fineID uuid.UUID
	var tenantID, payStatus, fineStatus, amount, currency, method string
	err = tx.QueryRow(r.Context(),
		`SELECT fp.fine_id, f.tenant_id, fp.status, f.status::text,
		        fp.amount::text, fp.currency, fp.method
		   FROM fine_payments fp
		   JOIN fines f ON f.id = fp.fine_id
		  WHERE fp.provider_ref = $1
		  FOR UPDATE`, evt.IntentID).
		Scan(&fineID, &tenantID, &payStatus, &fineStatus, &amount, &currency, &method)
	if errors.Is(err, pgx.ErrNoRows) {
		// Provider sent a webhook for an intent we don't track. ACK so
		// they stop retrying, but log it.
		a.log.Warn("webhook for unknown intent",
			slog.String("intent", evt.IntentID),
			slog.String("provider", a.pay.Info().Provider))
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}

	// Idempotency guard: already paid → 202 and out.
	if payStatus == "succeeded" || fineStatus == "paid" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if _, err := tx.Exec(r.Context(),
		`UPDATE fine_payments SET status='succeeded', paid_at=now() WHERE provider_ref=$1`,
		evt.IntentID); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE fines SET status='paid', paid_at=now() WHERE id=$1`, fineID); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	env := events.NewEnvelope("fines", tenantID, events.TypeFinePaid, 1,
		events.FinePaidPayload{
			FineID: fineID.String(), Amount: amount, Currency: currency,
			Method: method, ProviderRef: evt.IntentID,
		})
	if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	info := a.pay.Info()
	if a.hm != nil {
		_ = a.hm.OK(r.Context(), tenantID, info.Module, info.Provider, info.Region, nil)
	}
	_ = a.audit.Emit(r.Context(), "fine.pay.webhook", "fine", fineID.String(), nil,
		map[string]any{"intent": evt.IntentID, "provider": info.Provider})

	w.WriteHeader(http.StatusOK)
}

// runReaper drives one synchronous reaper sweep. Returns the count of
// rows sealed so ops can verify the trigger had work to do. 503 when
// the service was started without a reaper (tests do this).
func (a *API) runReaper(w http.ResponseWriter, r *http.Request) {
	if a.reap == nil {
		httpx.WriteErr(w, httpx.Err(http.StatusServiceUnavailable,
			"no_reaper", "reaper not wired"))
		return
	}
	n, err := a.reap.RunOnce(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sealed": n})
}

// paymentsHealth surfaces the per-tenant fail-streak for the bound
// payment provider so the admin /providers tile can render it next to
// ANPR / insurance / inspection. State / timestamps come from the same
// HealthMonitor those services use; if no payment has been attempted
// yet for this tenant the snapshot is empty and the timestamps are
// omitted from the response.
func (a *API) paymentsHealth(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	info := a.pay.Info()
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
			if !lastOK.IsZero() {
				resp["last_ok_at"] = lastOK
			}
			if !lastFail.IsZero() {
				resp["last_fail_at"] = lastFail
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
