// Vehicle ownership transfer handlers (citizen ↔ citizen).
//
// Flow:
//
//   seller:  POST /v1/citizens/me/vehicles/{vid}/transfer
//            body: { "to_contact": "buyer@…" }   → 201 { id, code, expires_at }
//
//   buyer:   POST /v1/citizens/me/transfers/accept
//            body: { "code": "ABC123" }          → 200 { vehicle_id }
//
//   seller:  POST /v1/citizens/me/transfers/{id}/cancel  → 204
//   either:  GET  /v1/citizens/me/transfers              → list
//
// What gets carried over to the buyer:
//   - the vehicle row (owner_id flips)
// What stays with the seller:
//   - any existing fines (driver_user_id is the snapshot at issue
//     time, owner_id was the snapshot too — those are immutable
//     facts about the *moment* the fine was issued)
//
// Why a code-and-not-an-account-link: a citizen may not have a portal
// account yet when they buy a car. The code is short-lived (7 days),
// single-use, and tenant-scoped, so the seller can hand it over via
// any channel without exposing their own credentials.
package api

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

const (
	transferCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // I, O, 0, 1 omitted
	transferCodeLen      = 6
	transferCodeRetries  = 5
)

// startTransfer is called by the seller. The seller must be the
// current owner of the vehicle (owner_id → owner row → user_id).
func (a *API) startTransfer(w http.ResponseWriter, r *http.Request) {
	vehicleID, err := uuid.Parse(r.PathValue("vid"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	type req struct {
		ToContact string `json:"to_contact"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if strings.TrimSpace(in.ToContact) == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing", "to_contact is required"))
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

	// Confirm the caller is the current owner of this vehicle. The
	// LEFT JOIN here is intentional — vehicles with no owner row aren't
	// transferable through this flow.
	var ownerID uuid.UUID
	err = tx.QueryRow(r.Context(),
		`SELECT o.id
		   FROM vehicles v
		   JOIN owners   o ON o.id = v.owner_id
		  WHERE v.id = $1 AND o.user_id = $2`,
		vehicleID, c.Subject).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrForbidden)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}

	// Pre-check: at most one open transfer per vehicle. Doing this
	// up-front avoids an aborted-tx state when the unique index fires
	// later — pgx can't run further queries on a failed tx.
	var existing uuid.UUID
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM vehicle_transfers
		  WHERE tenant_id=$1 AND vehicle_id=$2 AND status='pending'
		  LIMIT 1`, c.TenantID, vehicleID).Scan(&existing)
	if err == nil {
		httpx.WriteErr(w, httpx.Err(http.StatusConflict, "transfer_open",
			"a pending transfer already exists for this vehicle"))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, err)
		return
	}

	// Generate a code. The unique partial index on (tenant, code) WHERE
	// status='pending' will reject collisions; retry a few times. With
	// a 32^6 alphabet that's 1B+ codes, so retries past 1 are rare.
	// Each retry needs its own SAVEPOINT because pgx aborts the tx on
	// constraint violations.
	var code string
	var transferID uuid.UUID
	var expiresAt time.Time
	for i := 0; i < transferCodeRetries; i++ {
		code = randomCode()
		if _, err = tx.Exec(r.Context(), "SAVEPOINT t"); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		err = tx.QueryRow(r.Context(),
			`INSERT INTO vehicle_transfers
			   (tenant_id, vehicle_id, from_owner, to_contact, code)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id, expires_at`,
			c.TenantID, vehicleID, ownerID, in.ToContact, code).
			Scan(&transferID, &expiresAt)
		if err == nil {
			_, _ = tx.Exec(r.Context(), "RELEASE SAVEPOINT t")
			break
		}
		_, _ = tx.Exec(r.Context(), "ROLLBACK TO SAVEPOINT t")
		if !isUniqueViolation(err) {
			httpx.WriteErr(w, err)
			return
		}
		// Code collided — try a fresh one.
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	_ = a.audit.Emit(r.Context(), "vehicle.transfer.start", "vehicle",
		vehicleID.String(), nil,
		map[string]any{"transfer_id": transferID, "to_contact": in.ToContact})

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id":         transferID,
		"code":       code,
		"expires_at": expiresAt,
	})
}

// acceptTransfer is called by the buyer with the code. We require the
// caller to have an owners row in the same tenant — that's the
// destination of the transfer. (They'll typically have just called
// /v1/citizens/me/owner immediately before this.) The owner_id flip
// happens in one tx with the transfer row's status update.
func (a *API) acceptTransfer(w http.ResponseWriter, r *http.Request) {
	type req struct {
		Code string `json:"code"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	if in.Code == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing", "code is required"))
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

	// Resolve the buyer's owner row up front. If the buyer hasn't
	// self-claimed yet, the accept fails with 412 to nudge them
	// through the right flow.
	var buyerOwnerID uuid.UUID
	if err := tx.QueryRow(r.Context(),
		`SELECT id FROM owners WHERE user_id = $1`, c.Subject).Scan(&buyerOwnerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteErr(w, httpx.Err(http.StatusPreconditionFailed,
				"owner_required",
				"create your owner profile via /v1/citizens/me/owner first"))
			return
		}
		httpx.WriteErr(w, err)
		return
	}

	// Lock the transfer row. The unique index on (tenant, code) WHERE
	// status='pending' guarantees at most one match.
	var transferID, vehicleID, fromOwner uuid.UUID
	var expiresAt time.Time
	err = tx.QueryRow(r.Context(),
		`SELECT id, vehicle_id, from_owner, expires_at
		   FROM vehicle_transfers
		  WHERE code = $1 AND status = 'pending'
		  FOR UPDATE`, in.Code).
		Scan(&transferID, &vehicleID, &fromOwner, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if time.Now().After(expiresAt) {
		// Mark the row expired so subsequent attempts get a clean 404.
		_, _ = tx.Exec(r.Context(),
			`UPDATE vehicle_transfers SET status='expired' WHERE id=$1`, transferID)
		_ = tx.Commit(r.Context())
		httpx.WriteErr(w, httpx.Err(http.StatusGone, "expired",
			"transfer code has expired"))
		return
	}
	if buyerOwnerID == fromOwner {
		httpx.WriteErr(w, httpx.Err(http.StatusBadRequest, "self_transfer",
			"cannot accept your own transfer"))
		return
	}

	// Apply the transfer: flip vehicle.owner_id, mark the row accepted.
	if _, err := tx.Exec(r.Context(),
		`UPDATE vehicles SET owner_id=$2, updated_at=now() WHERE id=$1`,
		vehicleID, buyerOwnerID); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE vehicle_transfers
		    SET status='accepted', to_owner=$2, accepted_at=now()
		  WHERE id=$1`, transferID, buyerOwnerID); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	// Look up the plate inside the same tx so the event payload is
	// human-readable for downstream renderers (notifications, audit).
	var plate string
	_ = tx.QueryRow(r.Context(),
		`SELECT plate FROM vehicles WHERE id=$1`, vehicleID).Scan(&plate)
	env := events.EnvelopeFromContext(r.Context(), "registry", c.TenantID,
		events.TypeVehicleTransferred, 1,
		events.VehicleTransferredPayload{
			VehicleID: vehicleID.String(),
			Plate:     plate,
			FromOwner: fromOwner.String(),
			ToOwner:   buyerOwnerID.String(),
		})
	if err := events.WriteOutbox(r.Context(), tx, env); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	_ = a.audit.Emit(r.Context(), "vehicle.transfer.accept", "vehicle",
		vehicleID.String(),
		map[string]any{"owner_id": fromOwner},
		map[string]any{"owner_id": buyerOwnerID})

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"vehicle_id": vehicleID,
	})
}

// cancelTransfer lets the seller pull a pending transfer. Cancelling
// an already-cancelled / accepted / expired row is 404 — we don't
// silently overwrite a terminal state.
func (a *API) cancelTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()

	cmd, err := conn.Exec(r.Context(),
		`UPDATE vehicle_transfers t
		    SET status='cancelled'
		  WHERE t.id = $1
		    AND t.status = 'pending'
		    AND EXISTS (
		      SELECT 1 FROM owners o
		       WHERE o.id = t.from_owner AND o.user_id = $2)`,
		id, c.Subject)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if cmd.RowsAffected() == 0 {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listMyTransfers shows transfers the caller initiated. We return
// status, code, plate, contact, timestamps so the seller can chase the
// buyer if they didn't accept yet.
func (a *API) listMyTransfers(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()

	rows, err := conn.Query(r.Context(),
		`SELECT t.id, t.vehicle_id, v.plate, t.code, t.to_contact,
		        t.status, t.created_at, t.expires_at, t.accepted_at
		   FROM vehicle_transfers t
		   JOIN owners   o ON o.id = t.from_owner
		   JOIN vehicles v ON v.id = t.vehicle_id
		  WHERE o.user_id = $1
		  ORDER BY t.created_at DESC LIMIT 50`,
		c.Subject)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()

	type item struct {
		ID         uuid.UUID  `json:"id"`
		VehicleID  uuid.UUID  `json:"vehicle_id"`
		Plate      string     `json:"plate"`
		Code       string     `json:"code"`
		ToContact  string     `json:"to_contact"`
		Status     string     `json:"status"`
		CreatedAt  time.Time  `json:"created_at"`
		ExpiresAt  time.Time  `json:"expires_at"`
		AcceptedAt *time.Time `json:"accepted_at"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.VehicleID, &it.Plate, &it.Code,
			&it.ToContact, &it.Status, &it.CreatedAt, &it.ExpiresAt,
			&it.AcceptedAt); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		// Hide the code on terminal-status rows so a stale list doesn't
		// leak still-typeable codes.
		if it.Status != "pending" {
			it.Code = ""
		}
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// randomCode produces a transferCodeLen-character code from
// transferCodeAlphabet. Crypto/rand is mandatory: predictable codes
// would let an attacker accept a transfer they weren't given.
func randomCode() string {
	buf := make([]byte, transferCodeLen)
	if _, err := rand.Read(buf); err != nil {
		// Failing closed: a code we can't make securely is no code.
		// Caller will surface this as 500.
		panic("transfer: rand.Read: " + err.Error())
	}
	out := make([]byte, transferCodeLen)
	for i, b := range buf {
		out[i] = transferCodeAlphabet[int(b)%len(transferCodeAlphabet)]
	}
	return string(out)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx surfaces unique-violation as SQLSTATE 23505.
	type sqlState interface{ SQLState() string }
	if s, ok := err.(sqlState); ok {
		return s.SQLState() == "23505"
	}
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), "duplicate key")
}
