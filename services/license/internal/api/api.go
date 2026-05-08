// Package api wires the HTTP surface of the driver license service.
package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type API struct {
	cfg    config.Service
	log    *slog.Logger
	pool   *pgxpool.Pool
	issuer *auth.Issuer
	audit  *audit.Client
	bus    events.Publisher
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool,
	issuer *auth.Issuer, audit *audit.Client, bus events.Publisher) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, issuer: issuer, audit: audit, bus: bus}
	mux := http.NewServeMux()

	// Lifecycle. GET /v1/licenses?number=X is a search-shaped lookup so
	// it doesn't collide with /v1/licenses/{id}/* — Go 1.22 ServeMux
	// requires that any two patterns be unambiguously orderable.
	mux.Handle("POST /v1/licenses",
		issuer.Middleware(auth.RequirePermission("license:write")(http.HandlerFunc(a.create))))
	mux.Handle("GET /v1/licenses",
		issuer.Middleware(auth.RequirePermission("license:read")(http.HandlerFunc(a.list))))
	mux.Handle("GET /v1/licenses/{id}",
		issuer.Middleware(auth.RequirePermission("license:read")(http.HandlerFunc(a.get))))
	mux.Handle("PATCH /v1/licenses/{id}",
		issuer.Middleware(auth.RequirePermission("license:write")(http.HandlerFunc(a.update))))

	// Endorsements
	mux.Handle("POST /v1/licenses/{id}/endorsements",
		issuer.Middleware(auth.RequirePermission("license:write")(http.HandlerFunc(a.addEndorsement))))
	mux.Handle("GET  /v1/licenses/{id}/endorsements",
		issuer.Middleware(auth.RequirePermission("license:read")(http.HandlerFunc(a.listEndorsements))))

	// Suspensions
	mux.Handle("POST /v1/licenses/{id}/suspensions",
		issuer.Middleware(auth.RequirePermission("license:write")(http.HandlerFunc(a.addSuspension))))
	mux.Handle("POST /v1/licenses/{id}/suspensions/{sid}/lift",
		issuer.Middleware(auth.RequirePermission("license:write")(http.HandlerFunc(a.liftSuspension))))

	// Standing (computed) + violations
	mux.Handle("GET  /v1/licenses/{id}/standing",
		issuer.Middleware(http.HandlerFunc(a.standing)))
	mux.Handle("GET  /v1/licenses/{id}/violations",
		issuer.Middleware(http.HandlerFunc(a.violations)))

	// Biometric template registration (template stored in HSM/KMS — we keep a hash here).
	mux.Handle("POST /v1/licenses/{id}/biometrics",
		issuer.Middleware(auth.RequirePermission("license:write")(http.HandlerFunc(a.registerBiometric))))

	// Field verification — officer scans the citizen's QR/NFC code; we
	// return standing in <100ms. Public-but-rate-limited: tokens are
	// short-lived signed bundles, see verify.go.
	mux.Handle("POST /v1/licenses/verify",
		issuer.Middleware(http.HandlerFunc(a.verify)))
	mux.Handle("POST /v1/licenses/{id}/issue-token",
		issuer.Middleware(http.HandlerFunc(a.issueVerifyToken)))

	return mux
}

// ─── DTO ────────────────────────────────────────────────────────────────────
type license struct {
	ID            uuid.UUID  `json:"id"`
	UserID        *uuid.UUID `json:"user_id,omitempty"`
	LicenseNumber string     `json:"license_number"`
	FullName      string     `json:"full_name"`
	Classes       []string   `json:"classes"`
	IssuedAt      *string    `json:"issued_at,omitempty"`
	ExpiresAt     *string    `json:"expires_at,omitempty"`
	Points        int        `json:"points"`
	IsSuspended   bool       `json:"is_suspended"`
	SuspendedUntil *string   `json:"suspended_until,omitempty"`
}

type standing struct {
	License          license `json:"license"`
	Standing         string  `json:"standing"`           // good|expiring_soon|at_risk|expired|suspended
	RecentViolations int     `json:"recent_violations"`
	NextSuspensionThreshold int `json:"next_suspension_threshold,omitempty"`
}

// ─── Lifecycle ──────────────────────────────────────────────────────────────
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	type req struct {
		UserID        *uuid.UUID `json:"user_id"`
		LicenseNumber string     `json:"license_number"`
		FullName      string     `json:"full_name"`
		DateOfBirth   *string    `json:"date_of_birth"`
		Classes       []string   `json:"classes"`
		IssuedAt      *string    `json:"issued_at"`
		ExpiresAt     *string    `json:"expires_at"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if in.LicenseNumber == "" || in.FullName == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing_fields", "license_number and full_name are required"))
		return
	}
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	var id uuid.UUID
	err = conn.QueryRow(r.Context(),
		`INSERT INTO driver_licenses
		   (tenant_id, user_id, license_number, full_name, date_of_birth,
		    classes, issued_at, expires_at)
		 VALUES ($1,$2,$3,$4,$5::date,$6,$7::date,$8::date)
		 RETURNING id`,
		c.TenantID, in.UserID, in.LicenseNumber, in.FullName, in.DateOfBirth,
		in.Classes, in.IssuedAt, in.ExpiresAt,
	).Scan(&id)
	if err != nil { httpx.WriteErr(w, err); return }
	_ = a.audit.Emit(r.Context(), "license.create", "driver_license", id.String(), nil, in)
	uid := ""
	if in.UserID != nil { uid = in.UserID.String() }
	env := events.NewEnvelope("license", c.TenantID, events.TypeLicenseIssued, 1,
		events.LicenseIssuedPayload{
			LicenseID: id.String(), LicenseNumber: in.LicenseNumber,
			UserID: uid, Classes: in.Classes,
		})
	env.ActorID = c.Subject
	env.ActorRole = c.Role
	_ = a.bus.Publish(r.Context(), env)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	l, err := scanLicense(conn.QueryRow(r.Context(), licenseSelect+`WHERE id=$1`, id))
	if writeIfNotFound(w, err) { return }
	if err != nil { httpx.WriteErr(w, err); return }
	httpx.WriteJSON(w, http.StatusOK, l)
}

// list handles both "search" and "single-lookup" shapes:
//
//	GET /v1/licenses?number=DL-12345  → returns the matching license, or 404
//	GET /v1/licenses                  → reserved for paginated browse
//	                                    (Phase-3; returns empty list for now)
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()

	if num := r.URL.Query().Get("number"); num != "" {
		l, err := scanLicense(conn.QueryRow(r.Context(),
			licenseSelect+`WHERE license_number=$1`, num))
		if writeIfNotFound(w, err) { return }
		if err != nil { httpx.WriteErr(w, err); return }
		httpx.WriteJSON(w, http.StatusOK, l)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": []license{}})
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	type req struct {
		FullName  *string  `json:"full_name"`
		Classes   []string `json:"classes"`
		ExpiresAt *string  `json:"expires_at"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil { httpx.WriteErr(w, err); return }
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	if _, err := conn.Exec(r.Context(),
		`UPDATE driver_licenses SET
		   full_name  = COALESCE($2, full_name),
		   classes    = COALESCE($3, classes),
		   expires_at = COALESCE($4::date, expires_at)
		 WHERE id=$1`, id, in.FullName, in.Classes, in.ExpiresAt); err != nil {
		httpx.WriteErr(w, err); return
	}
	_ = a.audit.Emit(r.Context(), "license.update", "driver_license", id.String(), nil, in)
	w.WriteHeader(http.StatusNoContent)
}

// ─── Endorsements ───────────────────────────────────────────────────────────
func (a *API) addEndorsement(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	type req struct {
		Code        string  `json:"code"`
		Description string  `json:"description"`
		IssuedAt    string  `json:"issued_at"`
		ExpiresAt   *string `json:"expires_at"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil { httpx.WriteErr(w, err); return }
	if in.Code == "" || in.IssuedAt == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing_fields", "code and issued_at are required"))
		return
	}
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	var eid uuid.UUID
	err = conn.QueryRow(r.Context(),
		`INSERT INTO driver_endorsements (tenant_id, license_id, code, description, issued_at, expires_at)
		 VALUES ($1,$2,$3,$4,$5::date,$6::date) RETURNING id`,
		c.TenantID, id, in.Code, in.Description, in.IssuedAt, in.ExpiresAt).Scan(&eid)
	if err != nil { httpx.WriteErr(w, err); return }
	_ = a.audit.Emit(r.Context(), "license.endorsement.add", "driver_license", id.String(), nil, in)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": eid.String()})
}

func (a *API) listEndorsements(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT id, code, description, to_char(issued_at,'YYYY-MM-DD'),
		        to_char(expires_at,'YYYY-MM-DD')
		   FROM driver_endorsements WHERE license_id=$1
		  ORDER BY issued_at DESC`, id)
	if err != nil { httpx.WriteErr(w, err); return }
	defer rows.Close()
	type item struct {
		ID          uuid.UUID `json:"id"`
		Code        string    `json:"code"`
		Description *string   `json:"description"`
		IssuedAt    *string   `json:"issued_at"`
		ExpiresAt   *string   `json:"expires_at"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		_ = rows.Scan(&it.ID, &it.Code, &it.Description, &it.IssuedAt, &it.ExpiresAt)
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ─── Suspensions ────────────────────────────────────────────────────────────
func (a *API) addSuspension(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	type req struct {
		Reason      string  `json:"reason"`
		TriggerKind string  `json:"trigger_kind"` // demerit|court|medical|administrative
		StartsAt    *string `json:"starts_at"`
		EndsAt      *string `json:"ends_at"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil { httpx.WriteErr(w, err); return }
	if in.Reason == "" || in.TriggerKind == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing_fields", "reason and trigger_kind required"))
		return
	}
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	tx, err := conn.Begin(r.Context())
	if err != nil { httpx.WriteErr(w, err); return }
	defer tx.Rollback(r.Context())
	var sid uuid.UUID
	err = tx.QueryRow(r.Context(),
		`INSERT INTO driver_suspensions
		   (tenant_id, license_id, reason, trigger_kind, starts_at, ends_at, created_by)
		 VALUES ($1,$2,$3,$4, COALESCE($5::timestamptz, now()), $6::timestamptz, $7)
		 RETURNING id`,
		c.TenantID, id, in.Reason, in.TriggerKind, in.StartsAt, in.EndsAt, c.Subject).Scan(&sid)
	if err != nil { httpx.WriteErr(w, err); return }
	if _, err := tx.Exec(r.Context(),
		`UPDATE driver_licenses
		   SET is_suspended=true,
		       suspended_until = COALESCE($2::date, suspended_until)
		 WHERE id=$1`, id, in.EndsAt); err != nil {
		httpx.WriteErr(w, err); return
	}
	if err := tx.Commit(r.Context()); err != nil { httpx.WriteErr(w, err); return }

	_ = a.audit.Emit(r.Context(), "license.suspend", "driver_license", id.String(), nil, in)
	env := events.NewEnvelope("license", c.TenantID, events.TypeLicenseSuspended, 1,
		events.LicenseSuspendedPayload{
			LicenseID: id.String(), Reason: in.Reason, TriggerKind: in.TriggerKind,
			StartsAt: deref(in.StartsAt), EndsAt: deref(in.EndsAt),
		})
	env.ActorID = c.Subject
	env.ActorRole = c.Role
	_ = a.bus.Publish(r.Context(), env)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": sid.String()})
}

func (a *API) liftSuspension(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	sid, err := uuid.Parse(r.PathValue("sid"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	tx, err := conn.Begin(r.Context())
	if err != nil { httpx.WriteErr(w, err); return }
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`UPDATE driver_suspensions SET lifted_at=now()
		   WHERE id=$1 AND license_id=$2 AND lifted_at IS NULL`, sid, id); err != nil {
		httpx.WriteErr(w, err); return
	}
	// If no other active suspension, clear the cached flag.
	if _, err := tx.Exec(r.Context(),
		`UPDATE driver_licenses SET is_suspended=false, suspended_until=NULL
		   WHERE id=$1 AND NOT EXISTS (
		     SELECT 1 FROM driver_suspensions
		      WHERE license_id=$1 AND lifted_at IS NULL
		        AND (ends_at IS NULL OR ends_at > now()))`, id); err != nil {
		httpx.WriteErr(w, err); return
	}
	if err := tx.Commit(r.Context()); err != nil { httpx.WriteErr(w, err); return }
	_ = a.audit.Emit(r.Context(), "license.reinstate", "driver_license", id.String(), nil,
		map[string]string{"suspension_id": sid.String()})
	env := events.NewEnvelope("license", c.TenantID, events.TypeLicenseReinstated, 1,
		map[string]string{"license_id": id.String(), "suspension_id": sid.String()})
	env.ActorID = c.Subject
	env.ActorRole = c.Role
	_ = a.bus.Publish(r.Context(), env)
	w.WriteHeader(http.StatusNoContent)
}

// ─── Standing / violations ──────────────────────────────────────────────────
func (a *API) standing(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()

	l, err := scanLicense(conn.QueryRow(r.Context(), licenseSelect+`WHERE id=$1`, id))
	if writeIfNotFound(w, err) { return }
	if err != nil { httpx.WriteErr(w, err); return }

	var st string
	var recent int
	if err := conn.QueryRow(r.Context(),
		`SELECT standing, recent_violations FROM v_driver_standing WHERE license_id=$1`, id).
		Scan(&st, &recent); err != nil {
		httpx.WriteErr(w, err); return
	}
	var threshold int
	_ = conn.QueryRow(r.Context(),
		`SELECT threshold_points FROM driver_demerit_policy WHERE tenant_id=app_tenant()`).
		Scan(&threshold)
	httpx.WriteJSON(w, http.StatusOK, standing{
		License: l, Standing: st, RecentViolations: recent,
		NextSuspensionThreshold: threshold,
	})
}

func (a *API) violations(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT id, fine_id, offence_code, points, occurred_at, recorded_at
		   FROM driver_violations WHERE license_id=$1
		  ORDER BY occurred_at DESC`, id)
	if err != nil { httpx.WriteErr(w, err); return }
	defer rows.Close()
	type v struct {
		ID          uuid.UUID  `json:"id"`
		FineID      *uuid.UUID `json:"fine_id"`
		OffenceCode string     `json:"offence_code"`
		Points      int        `json:"points"`
		OccurredAt  time.Time  `json:"occurred_at"`
		RecordedAt  time.Time  `json:"recorded_at"`
	}
	out := []v{}
	for rows.Next() {
		var it v
		_ = rows.Scan(&it.ID, &it.FineID, &it.OffenceCode, &it.Points, &it.OccurredAt, &it.RecordedAt)
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ─── Biometrics ─────────────────────────────────────────────────────────────
func (a *API) registerBiometric(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	type req struct {
		TemplateKid string `json:"template_kid"` // KMS/HSM key id
		Algo        string `json:"algo"`          // 'iso19794-fingerprint','face-embed-v1', ...
		// TemplateB64 is the raw template body. We immediately hash it
		// and DROP the plaintext; only the kid + sha256 stay in this DB.
		TemplateB64 string `json:"template_b64"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil { httpx.WriteErr(w, err); return }
	if in.TemplateKid == "" || in.Algo == "" || in.TemplateB64 == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing_fields", "template_kid, algo, template_b64 required"))
		return
	}
	tplt, err := base64.StdEncoding.DecodeString(in.TemplateB64)
	if err != nil { httpx.WriteErr(w, httpx.Err(400, "bad_b64", "template_b64 is invalid base64")); return }
	sum := sha256.Sum256(tplt)
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	if _, err := conn.Exec(r.Context(),
		`INSERT INTO driver_biometrics (license_id, tenant_id, template_kid, template_hash, algo)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (license_id) DO UPDATE
		   SET template_kid=$3, template_hash=$4, algo=$5, enrolled_at=now()`,
		id, c.TenantID, in.TemplateKid, sum[:], in.Algo); err != nil {
		httpx.WriteErr(w, err); return
	}
	_ = a.audit.Emit(r.Context(), "license.biometric.register", "driver_license", id.String(), nil,
		map[string]string{"algo": in.Algo, "template_hash": hex.EncodeToString(sum[:])})
	w.WriteHeader(http.StatusNoContent)
}

// ─── helpers ────────────────────────────────────────────────────────────────
const licenseSelect = `
SELECT id, user_id, license_number, full_name, classes,
       to_char(issued_at,'YYYY-MM-DD'), to_char(expires_at,'YYYY-MM-DD'),
       points, is_suspended, to_char(suspended_until,'YYYY-MM-DD')
  FROM driver_licenses `

func scanLicense(row pgx.Row) (license, error) {
	var l license
	err := row.Scan(&l.ID, &l.UserID, &l.LicenseNumber, &l.FullName,
		&l.Classes, &l.IssuedAt, &l.ExpiresAt,
		&l.Points, &l.IsSuspended, &l.SuspendedUntil)
	return l, err
}

func writeIfNotFound(w http.ResponseWriter, err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return true
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
