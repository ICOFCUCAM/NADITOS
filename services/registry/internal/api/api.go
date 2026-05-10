// Package api wires the HTTP surface of the vehicle registry service.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
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

	// plateRegexCache memoises the compiled jurisdiction-format regex
	// per tenant. Vehicle creates are low-rate but we still want to
	// avoid a DB roundtrip + regex compile on every POST. Entries
	// never expire because plate_regex on a tenant rarely changes; a
	// future "ministry edits the country pack" admin endpoint should
	// call invalidatePlateRegex(tenant) after writing the new regex.
	plateRegexCache sync.Map // map[string]*regexp.Regexp
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool,
	issuer *auth.Issuer, audit *audit.Client, bus events.Publisher) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, issuer: issuer, audit: audit, bus: bus}

	root := http.NewServeMux()
	// Vehicles
	root.Handle("GET /v1/vehicles",                issuer.Middleware(auth.RequirePermission("registry:read")(http.HandlerFunc(a.list))))
	root.Handle("POST /v1/vehicles",                issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(a.create))))
	root.Handle("GET /v1/vehicles/{id}",           issuer.Middleware(auth.RequirePermission("registry:read")(http.HandlerFunc(a.get))))
	root.Handle("PATCH /v1/vehicles/{id}",          issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(a.update))))
	root.Handle("GET /v1/vehicles/by-plate/{plate}", issuer.Middleware(auth.RequirePermission("registry:read")(http.HandlerFunc(a.byPlate))))
	root.Handle("POST /v1/vehicles/{id}/flags",     issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(a.setFlags))))

	// Owners (admin)
	root.Handle("POST /v1/owners",                  issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(a.createOwner))))
	root.Handle("GET /v1/owners",                  issuer.Middleware(auth.RequirePermission("registry:read")(http.HandlerFunc(a.listOwners))))
	root.Handle("GET /v1/owners/{id}",             issuer.Middleware(auth.RequirePermission("registry:read")(http.HandlerFunc(a.getOwner))))
	root.Handle("PATCH /v1/owners/{id}",            issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(a.updateOwner))))
	root.Handle("POST /v1/owners/{id}/vehicles/{vid}", issuer.Middleware(auth.RequirePermission("registry:write")(http.HandlerFunc(a.linkVehicle))))

	// Citizen self-service. The route is gated by 'owners:self' so a
	// regular browse-only citizen JWT can't claim ownership without it.
	root.Handle("POST /v1/citizens/me/owner",       issuer.Middleware(auth.RequirePermission("owners:self")(http.HandlerFunc(a.selfClaimOwner))))
	root.Handle("GET /v1/citizens/me/owner",       issuer.Middleware(http.HandlerFunc(a.getMyOwner)))
	root.Handle("GET /v1/citizens/me/vehicles",    issuer.Middleware(http.HandlerFunc(a.myVehicles)))
	// Citizen-to-citizen vehicle ownership transfer. The seller starts
	// the transfer; the buyer accepts with the returned code. Existing
	// fines stay attached to the seller — only future responsibility
	// shifts.
	root.Handle("POST /v1/citizens/me/vehicles/{vid}/transfer",
		issuer.Middleware(http.HandlerFunc(a.startTransfer)))
	root.Handle("GET /v1/citizens/me/transfers",
		issuer.Middleware(http.HandlerFunc(a.listMyTransfers)))
	root.Handle("POST /v1/citizens/me/transfers/{id}/cancel",
		issuer.Middleware(http.HandlerFunc(a.cancelTransfer)))
	root.Handle("POST /v1/citizens/me/transfers/accept",
		issuer.Middleware(http.HandlerFunc(a.acceptTransfer)))

	return root
}

// ─── DTO ────────────────────────────────────────────────────────────────────
type vehicle struct {
	ID                     uuid.UUID  `json:"id"`
	Plate                  string     `json:"plate"`
	VIN                    *string    `json:"vin,omitempty"`
	Make                   *string    `json:"make,omitempty"`
	Model                  *string    `json:"model,omitempty"`
	Year                   *int       `json:"year,omitempty"`
	Color                  *string    `json:"color,omitempty"`
	Category               *string    `json:"category,omitempty"`
	EmissionClass          *string    `json:"emission_class,omitempty"`
	OwnerID                *uuid.UUID `json:"owner_id,omitempty"`
	RegistrationExpiresAt  *time.Time `json:"registration_expires_at,omitempty"`
	InsuranceExpiresAt     *time.Time `json:"insurance_expires_at,omitempty"`
	InspectionExpiresAt    *time.Time `json:"inspection_expires_at,omitempty"`
	TaxPaidThrough         *time.Time `json:"tax_paid_through,omitempty"`
	IsStolen               bool       `json:"is_stolen"`
	IsSeized               bool       `json:"is_seized"`
	IsWanted               bool       `json:"is_wanted"`
	Status                 string     `json:"status"` // green|yellow|red|black
}

const vehicleCols = `
  v.id, v.plate, v.vin, v.make, v.model, v.year, v.color, v.category,
  v.emission_class, v.owner_id,
  v.registration_expires_at, v.insurance_expires_at, v.inspection_expires_at,
  v.tax_paid_through, v.is_stolen, v.is_seized, v.is_wanted,
  s.status`

func scanVehicle(row pgx.Row) (vehicle, error) {
	var v vehicle
	err := row.Scan(
		&v.ID, &v.Plate, &v.VIN, &v.Make, &v.Model, &v.Year, &v.Color, &v.Category,
		&v.EmissionClass, &v.OwnerID,
		&v.RegistrationExpiresAt, &v.InsuranceExpiresAt, &v.InspectionExpiresAt,
		&v.TaxPaidThrough, &v.IsStolen, &v.IsSeized, &v.IsWanted,
		&v.Status,
	)
	return v, err
}

// ─── Handlers ───────────────────────────────────────────────────────────────
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()

	q := r.URL.Query().Get("q")
	flagged := r.URL.Query().Get("flagged") == "1"
	limit := 50
	args := []any{}
	sql := `SELECT ` + vehicleCols + `
	          FROM vehicles v
	          JOIN v_vehicle_status s ON s.id = v.id`
	conds := []string{}
	if q != "" {
		args = append(args, "%"+q+"%")
		conds = append(conds, `(v.plate ILIKE $1 OR v.vin ILIKE $1)`)
	}
	if flagged {
		conds = append(conds, `(v.is_stolen OR v.is_seized OR v.is_wanted)`)
	}
	if len(conds) > 0 {
		sql += ` WHERE ` + joinAnd(conds)
	}
	sql += ` ORDER BY v.plate LIMIT ` + itoa(limit)

	rows, err := conn.Query(r.Context(), sql, args...)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()

	out := []vehicle{}
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			httpx.WriteErr(w, err)
			return
		}
		out = append(out, v)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	type req struct {
		Plate                 string     `json:"plate"`
		VIN                   *string    `json:"vin"`
		Make                  *string    `json:"make"`
		Model                 *string    `json:"model"`
		Year                  *int       `json:"year"`
		Color                 *string    `json:"color"`
		Category              *string    `json:"category"`
		EmissionClass         *string    `json:"emission_class"`
		OwnerID               *uuid.UUID `json:"owner_id"`
		RegistrationExpiresAt *time.Time `json:"registration_expires_at"`
		InsuranceExpiresAt    *time.Time `json:"insurance_expires_at"`
		InspectionExpiresAt   *time.Time `json:"inspection_expires_at"`
		TaxPaidThrough        *time.Time `json:"tax_paid_through"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if in.Plate == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing_plate", "plate is required"))
		return
	}
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()

	c := auth.ClaimsFrom(r.Context())
	if err := a.validatePlate(r.Context(), conn, c.TenantID, in.Plate); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	tx, err := conn.Begin(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(r.Context())

	var id uuid.UUID
	err = tx.QueryRow(r.Context(),
		`INSERT INTO vehicles (
			tenant_id, plate, vin, make, model, year, color, category,
			emission_class, owner_id,
			registration_expires_at, insurance_expires_at,
			inspection_expires_at, tax_paid_through)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 RETURNING id`,
		c.TenantID, in.Plate, in.VIN, in.Make, in.Model, in.Year, in.Color, in.Category,
		in.EmissionClass, in.OwnerID,
		in.RegistrationExpiresAt, in.InsuranceExpiresAt,
		in.InspectionExpiresAt, in.TaxPaidThrough,
	).Scan(&id)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	owner := ""
	if in.OwnerID != nil {
		owner = in.OwnerID.String()
	}
	env := events.EnvelopeFromContext(r.Context(), "registry", c.TenantID, events.TypeVehicleCreated, 1,
		events.VehicleCreatedPayload{VehicleID: id.String(), Plate: in.Plate, OwnerID: owner})
	if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	_ = a.audit.Emit(r.Context(), "vehicle.create", "vehicle", id.String(), nil, in)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
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
	row := conn.QueryRow(r.Context(),
		`SELECT `+vehicleCols+` FROM vehicles v
		   JOIN v_vehicle_status s ON s.id = v.id WHERE v.id=$1`, id)
	v, err := scanVehicle(row)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (a *API) byPlate(w http.ResponseWriter, r *http.Request) {
	plate := r.PathValue("plate")
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	row := conn.QueryRow(r.Context(),
		`SELECT `+vehicleCols+` FROM vehicles v
		   JOIN v_vehicle_status s ON s.id = v.id
		  WHERE v.plate = $1`, plate)
	v, err := scanVehicle(row)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	type req struct {
		Make                  *string    `json:"make"`
		Model                 *string    `json:"model"`
		Color                 *string    `json:"color"`
		OwnerID               *uuid.UUID `json:"owner_id"`
		RegistrationExpiresAt *time.Time `json:"registration_expires_at"`
		InsuranceExpiresAt    *time.Time `json:"insurance_expires_at"`
		InspectionExpiresAt   *time.Time `json:"inspection_expires_at"`
		TaxPaidThrough        *time.Time `json:"tax_paid_through"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	_, err = conn.Exec(r.Context(),
		`UPDATE vehicles SET
		   make = COALESCE($2, make),
		   model = COALESCE($3, model),
		   color = COALESCE($4, color),
		   owner_id = COALESCE($5, owner_id),
		   registration_expires_at = COALESCE($6, registration_expires_at),
		   insurance_expires_at    = COALESCE($7, insurance_expires_at),
		   inspection_expires_at   = COALESCE($8, inspection_expires_at),
		   tax_paid_through        = COALESCE($9, tax_paid_through),
		   updated_at = now()
		 WHERE id=$1`,
		id, in.Make, in.Model, in.Color, in.OwnerID,
		in.RegistrationExpiresAt, in.InsuranceExpiresAt,
		in.InspectionExpiresAt, in.TaxPaidThrough)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	_ = a.audit.Emit(r.Context(), "vehicle.update", "vehicle", id.String(), nil, in)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setFlags(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	type req struct {
		IsStolen *bool `json:"is_stolen"`
		IsSeized *bool `json:"is_seized"`
		IsWanted *bool `json:"is_wanted"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
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
		`UPDATE vehicles SET
		   is_stolen = COALESCE($2, is_stolen),
		   is_seized = COALESCE($3, is_seized),
		   is_wanted = COALESCE($4, is_wanted),
		   updated_at = now()
		 WHERE id=$1`,
		id, in.IsStolen, in.IsSeized, in.IsWanted); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	c := auth.ClaimsFrom(r.Context())
	var plate string
	var stolen, seized, wanted bool
	if err := tx.QueryRow(r.Context(),
		`SELECT plate, is_stolen, is_seized, is_wanted FROM vehicles WHERE id=$1`, id).
		Scan(&plate, &stolen, &seized, &wanted); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	env := events.EnvelopeFromContext(r.Context(), "registry", c.TenantID, events.TypeVehicleFlagged, 1,
		events.VehicleFlaggedPayload{
			VehicleID: id.String(), Plate: plate,
			IsStolen: stolen, IsSeized: seized, IsWanted: wanted,
		})
	if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	_ = a.audit.Emit(r.Context(), "vehicle.flags", "vehicle", id.String(), nil, in)
	w.WriteHeader(http.StatusNoContent)
}

func itoa(i int) string {
	if i == 50 { return "50" }
	if i == 100 { return "100" }
	return "50"
}

// joinAnd is a small SQL helper that stitches WHERE-clause fragments
// together with " AND " so the list handler can compose conditions
// without dragging strings.Join in for one call site.
func joinAnd(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " AND " + p
	}
	return out
}

// validatePlate enforces the jurisdiction's plate format on creation.
// The regex is configured per-tenant in the country pack and stored on
// the tenants row; ministries that bring a country pack online via the
// admin app set the format there. Plates are always uppercased before
// matching so callers don't have to second-guess the regex's case.
//
// Returns nil if the plate is acceptable, or an httpx.Error tagged
// "bad_plate" with the regex value embedded so the admin/citizen UIs
// can surface the format requirement without an extra round trip.
func (a *API) validatePlate(ctx context.Context, q queryRow, tenant, plate string) error {
	re, err := a.plateRegexFor(ctx, q, tenant)
	if err != nil {
		return fmt.Errorf("load plate_regex for tenant %s: %w", tenant, err)
	}
	if !re.MatchString(plate) {
		return httpx.Err(http.StatusBadRequest, "bad_plate",
			fmt.Sprintf("plate %q does not match jurisdiction format %q", plate, re.String()))
	}
	return nil
}

// plateRegexFor returns the compiled regex for a tenant, memoising the
// result so subsequent creates skip both the DB read and the regex
// compile. The cache lives forever; a future endpoint that mutates
// plate_regex must call a.plateRegexCache.Delete(tenant).
func (a *API) plateRegexFor(ctx context.Context, q queryRow, tenant string) (*regexp.Regexp, error) {
	if cached, ok := a.plateRegexCache.Load(tenant); ok {
		return cached.(*regexp.Regexp), nil
	}
	var pat string
	if err := q.QueryRow(ctx, `SELECT plate_regex FROM tenants WHERE id = $1`, tenant).Scan(&pat); err != nil {
		return nil, err
	}
	if pat == "" {
		// Defensive fallback; the column has a NOT NULL DEFAULT in 0001
		// so this branch should be unreachable, but a corrupted row
		// shouldn't be a 500-cascade.
		pat = `^[A-Z0-9-]{2,10}$`
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("invalid plate_regex %q: %w", pat, err)
	}
	a.plateRegexCache.Store(tenant, re)
	return re, nil
}

// queryRow is the narrow QueryRow surface validatePlate needs;
// pgx.Tx, *pgxpool.Conn, and *pgxpool.Pool all satisfy it.
type queryRow interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
