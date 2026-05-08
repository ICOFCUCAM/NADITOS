// Owner handlers for the registry service.
//
// Owners represent the human (or organization) who legally owns one or
// more vehicles. They optionally link to a `users` row when the owner
// has signed up for the citizen portal — that's how the notifications
// service knows the email and phone for fine notices.
//
// Access pattern:
//
//   admin: POST /v1/owners               — create
//          GET  /v1/owners?q=…           — search
//          GET  /v1/owners/{id}          — read
//          PATCH /v1/owners/{id}         — update contact info
//          POST /v1/owners/{id}/vehicles/{vid}  — link vehicle
//
//   citizen: POST /v1/citizens/me/owner  — self-claim (idempotent)
//            GET  /v1/citizens/me/vehicles — list mine
package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

// ─── DTOs ───────────────────────────────────────────────────────────────────
type owner struct {
	ID         uuid.UUID  `json:"id"`
	UserID     *uuid.UUID `json:"user_id,omitempty"`
	FullName   string     `json:"full_name"`
	NationalID *string    `json:"national_id,omitempty"`
	Email      *string    `json:"email,omitempty"`
	Phone      *string    `json:"phone,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

const ownerCols = `id, user_id, full_name, national_id, email::text, phone, created_at`

func scanOwner(row pgx.Row) (owner, error) {
	var o owner
	err := row.Scan(&o.ID, &o.UserID, &o.FullName, &o.NationalID, &o.Email, &o.Phone, &o.CreatedAt)
	return o, err
}

// ─── Admin handlers ─────────────────────────────────────────────────────────
func (a *API) createOwner(w http.ResponseWriter, r *http.Request) {
	type req struct {
		UserID     *uuid.UUID `json:"user_id"`
		FullName   string     `json:"full_name"`
		NationalID *string    `json:"national_id"`
		Email      *string    `json:"email"`
		Phone      *string    `json:"phone"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if strings.TrimSpace(in.FullName) == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing", "full_name is required"))
		return
	}

	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()

	var id uuid.UUID
	err = conn.QueryRow(r.Context(),
		`INSERT INTO owners (tenant_id, user_id, full_name, national_id, email, phone)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		c.TenantID, in.UserID, in.FullName, in.NationalID, in.Email, in.Phone).Scan(&id)
	if err != nil { httpx.WriteErr(w, err); return }
	_ = a.audit.Emit(r.Context(), "owner.create", "owner", id.String(), nil, in)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (a *API) listOwners(w http.ResponseWriter, r *http.Request) {
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	args := []any{}
	sql := `SELECT ` + ownerCols + ` FROM owners`
	if q != "" {
		args = append(args, "%"+q+"%")
		sql += ` WHERE full_name ILIKE $1 OR email::text ILIKE $1 OR national_id ILIKE $1`
	}
	sql += ` ORDER BY full_name LIMIT 100`

	rows, err := conn.Query(r.Context(), sql, args...)
	if err != nil { httpx.WriteErr(w, err); return }
	defer rows.Close()
	out := []owner{}
	for rows.Next() {
		o, err := scanOwner(rows)
		if err != nil { httpx.WriteErr(w, err); return }
		out = append(out, o)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (a *API) getOwner(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	o, err := scanOwner(conn.QueryRow(r.Context(),
		`SELECT `+ownerCols+` FROM owners WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound); return
	}
	if err != nil { httpx.WriteErr(w, err); return }
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (a *API) updateOwner(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	type req struct {
		FullName *string `json:"full_name"`
		Email    *string `json:"email"`
		Phone    *string `json:"phone"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err); return
	}
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	if _, err := conn.Exec(r.Context(),
		`UPDATE owners SET
		   full_name = COALESCE($2, full_name),
		   email     = COALESCE($3, email),
		   phone     = COALESCE($4, phone)
		 WHERE id=$1`, id, in.FullName, in.Email, in.Phone); err != nil {
		httpx.WriteErr(w, err); return
	}
	_ = a.audit.Emit(r.Context(), "owner.update", "owner", id.String(), nil, in)
	w.WriteHeader(http.StatusNoContent)
}

// linkVehicle assigns a vehicle to an owner. The previous owner_id (if
// any) is recorded in the audit envelope as `before` so the chain of
// custody is preserved.
func (a *API) linkVehicle(w http.ResponseWriter, r *http.Request) {
	ownerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }
	vehicleID, err := uuid.Parse(r.PathValue("vid"))
	if err != nil { httpx.WriteErr(w, httpx.ErrBadRequest); return }

	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	tx, err := conn.Begin(r.Context())
	if err != nil { httpx.WriteErr(w, err); return }
	defer tx.Rollback(r.Context())

	var prev *uuid.UUID
	if err := tx.QueryRow(r.Context(),
		`SELECT owner_id FROM vehicles WHERE id=$1`, vehicleID).Scan(&prev); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteErr(w, httpx.ErrNotFound); return
		}
		httpx.WriteErr(w, err); return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE vehicles SET owner_id=$2, updated_at=now() WHERE id=$1`,
		vehicleID, ownerID); err != nil {
		httpx.WriteErr(w, err); return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err); return
	}
	_ = a.audit.Emit(r.Context(), "vehicle.transfer", "vehicle", vehicleID.String(),
		map[string]any{"owner_id": prev},
		map[string]any{"owner_id": ownerID})
	w.WriteHeader(http.StatusNoContent)
}

// ─── Citizen self-service ───────────────────────────────────────────────────

// selfClaimOwner is idempotent: the citizen calls it once after signup
// to create their owners row, or repeatedly to update contact info.
// The user_id is taken from the JWT — never from the body — so a
// citizen cannot impersonate another user.
func (a *API) selfClaimOwner(w http.ResponseWriter, r *http.Request) {
	type req struct {
		FullName   string  `json:"full_name"`
		NationalID *string `json:"national_id"`
		Email      *string `json:"email"`
		Phone      *string `json:"phone"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err); return
	}
	if strings.TrimSpace(in.FullName) == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing", "full_name is required"))
		return
	}
	c := auth.ClaimsFrom(r.Context())
	userID, err := uuid.Parse(c.Subject)
	if err != nil {
		httpx.WriteErr(w, httpx.ErrUnauthorized); return
	}

	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()

	// Upsert by user_id within the tenant. The unique index on
	// (tenant_id, user_id) is partial (WHERE user_id IS NOT NULL), so
	// the ON CONFLICT clause must repeat that predicate for inference
	// to succeed.
	var id uuid.UUID
	err = conn.QueryRow(r.Context(),
		`INSERT INTO owners (tenant_id, user_id, full_name, national_id, email, phone)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id, user_id) WHERE user_id IS NOT NULL DO UPDATE
		   SET full_name = EXCLUDED.full_name,
		       national_id = COALESCE(EXCLUDED.national_id, owners.national_id),
		       email     = COALESCE(EXCLUDED.email, owners.email),
		       phone     = COALESCE(EXCLUDED.phone, owners.phone)
		 RETURNING id`,
		c.TenantID, userID, in.FullName, in.NationalID, in.Email, in.Phone).Scan(&id)
	if err != nil { httpx.WriteErr(w, err); return }
	_ = a.audit.Emit(r.Context(), "owner.self_claim", "owner", id.String(), nil, in)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"id": id.String()})
}

// myVehicles lists the vehicles owned by the citizen behind the JWT.
func (a *API) myVehicles(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT `+vehicleCols+`
		   FROM vehicles v
		   JOIN v_vehicle_status s ON s.id = v.id
		   JOIN owners o ON o.id = v.owner_id
		  WHERE o.user_id = $1
		  ORDER BY v.plate`, c.Subject)
	if err != nil { httpx.WriteErr(w, err); return }
	defer rows.Close()
	out := []vehicle{}
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil { httpx.WriteErr(w, err); return }
		out = append(out, v)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}
