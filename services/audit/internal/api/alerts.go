// Audit alert endpoints. The rollup writes anomaly rows into
// audit_alerts; admins triage them through these handlers.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type alertOut struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	SubjectKind *string         `json:"subject_kind"`
	SubjectID   *string         `json:"subject_id"`
	Day         time.Time       `json:"day"`
	Severity    *float32        `json:"severity"`
	Details     json.RawMessage `json:"details"`
	DetectedAt  time.Time       `json:"detected_at"`
	ResolvedAt  *time.Time      `json:"resolved_at"`
	Resolution  *string         `json:"resolution"`
}

// listAlerts returns OPEN alerts for the caller's tenant by default;
// pass ?include_resolved=1 to see triaged history. Newest first so the
// dashboard shows fresh signal at the top.
func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	includeResolved := r.URL.Query().Get("include_resolved") == "1"

	conn, err := a.pool.Acquire(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	_, _ = conn.Exec(r.Context(), "SET LOCAL row_security = off")

	q := `SELECT id, kind, subject_kind, subject_id::text, day, severity,
	             details, detected_at, resolved_at, resolution
	        FROM audit_alerts
	       WHERE tenant_id = $1`
	if !includeResolved {
		q += ` AND resolved_at IS NULL`
	}
	q += ` ORDER BY detected_at DESC LIMIT 200`

	rows, err := conn.Query(r.Context(), q, c.TenantID)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()

	out := []alertOut{}
	for rows.Next() {
		var it alertOut
		if err := rows.Scan(&it.ID, &it.Kind, &it.SubjectKind, &it.SubjectID,
			&it.Day, &it.Severity, &it.Details, &it.DetectedAt,
			&it.ResolvedAt, &it.Resolution); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// resolveAlert closes an alert with a free-text resolution note and
// stamps the admin's user id. Only OPEN alerts can be resolved — a
// second call against a resolved alert is 404 so we don't silently
// overwrite someone else's triage.
func (a *API) resolveAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	var in struct {
		Resolution string `json:"resolution"`
	}
	_ = httpx.ReadJSON(r, &in)

	c := auth.ClaimsFrom(r.Context())
	actor, _ := uuid.Parse(c.Subject)

	conn, err := a.pool.Acquire(r.Context())
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
	if _, err := tx.Exec(r.Context(), "SET LOCAL row_security = off"); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	var dummy int64
	err = tx.QueryRow(r.Context(),
		`UPDATE audit_alerts
		    SET resolved_at = now(),
		        resolved_by = $2,
		        resolution  = NULLIF($3,'')
		  WHERE id=$1
		    AND tenant_id=$4
		    AND resolved_at IS NULL
		  RETURNING id`,
		id, actor, in.Resolution, c.TenantID).Scan(&dummy)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(w, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
