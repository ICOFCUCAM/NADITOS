// Citizen self-service license views.
//
// GET /v1/citizens/me/license returns the caller's own driver license
// (matched by user_id from the JWT) with recent demerit events and
// suspension history. No admin permission required — the user_id
// filter keeps citizens from reading anyone else's license.
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type demeritEvent struct {
	OccurredAt time.Time `json:"occurred_at"`
	Delta      int       `json:"delta"`
	Reason     string    `json:"reason"`
	Source     string    `json:"source"`
	NewTotal   int       `json:"new_total"`
}

type suspensionRow struct {
	ID          string     `json:"id"`
	Reason      string     `json:"reason"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      time.Time  `json:"ends_at"`
	LiftedAt    *time.Time `json:"lifted_at"`
	TriggerKind string     `json:"trigger_kind"`
}

// myLicense returns the citizen's own license bundle: license + standing
// summary + last 50 demerit events + last 10 suspensions. A citizen with
// no license row gets 404 so the UI can render an empty state.
func (a *API) myLicense(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()

	l, err := scanLicense(conn.QueryRow(r.Context(),
		licenseSelect+`WHERE user_id=$1::uuid`, c.Subject))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}

	// Standing pulls from the materialized view; if it's empty for this
	// license (newly issued, no fines yet) we fall through with default
	// "good" so the response shape is stable.
	var st string
	var recent int
	if err := conn.QueryRow(r.Context(),
		`SELECT standing, recent_violations
		   FROM v_driver_standing WHERE license_id=$1::uuid`, l.ID).
		Scan(&st, &recent); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, err)
		return
	}
	if st == "" {
		st = "good"
	}

	// Demerit history. The cumulative total runs oldest → newest, so
	// the loop sums forward and tags each row's NewTotal with the
	// running count.
	rows, err := conn.Query(r.Context(),
		`SELECT occurred_at, delta, reason, source
		   FROM driver_demerit_events
		  WHERE license_id=$1::uuid
		  ORDER BY occurred_at DESC LIMIT 50`, l.ID)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()
	demerits := []demeritEvent{}
	for rows.Next() {
		var d demeritEvent
		if err := rows.Scan(&d.OccurredAt, &d.Delta, &d.Reason, &d.Source); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		demerits = append(demerits, d)
	}
	rows.Close()
	running := 0
	for i := len(demerits) - 1; i >= 0; i-- {
		running += demerits[i].Delta
		demerits[i].NewTotal = running
	}

	// Suspension history.
	srows, err := conn.Query(r.Context(),
		`SELECT id::text, reason, starts_at, ends_at, lifted_at,
		        COALESCE(trigger_kind,'')
		   FROM driver_suspensions
		  WHERE license_id=$1::uuid
		  ORDER BY starts_at DESC LIMIT 10`, l.ID)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer srows.Close()
	suspensions := []suspensionRow{}
	for srows.Next() {
		var s suspensionRow
		if err := srows.Scan(&s.ID, &s.Reason, &s.StartsAt, &s.EndsAt,
			&s.LiftedAt, &s.TriggerKind); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		suspensions = append(suspensions, s)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"license":           l,
		"standing":          st,
		"recent_violations": recent,
		"demerits":          demerits,
		"suspensions":       suspensions,
	})
}
