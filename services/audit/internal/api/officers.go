package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

// officerStats returns the officer_daily_stats rows for the caller's
// tenant within the last `days` days (default 14). The result is sorted
// by anomaly_score desc so outliers float to the top of the dashboard.
//
// We bypass RLS (audit pool runs with BYPASSRLS by design) but enforce
// tenant scope here in code via the JWT claim. Without that the audit
// admin would see other tenants' rows.
func (a *API) officerStats(w http.ResponseWriter, r *http.Request) {
	c := auth.ClaimsFrom(r.Context())
	days := 14
	conn, err := a.pool.Acquire(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	rows, err := conn.Query(r.Context(),
		`SELECT s.officer_id, COALESCE(u.full_name, ''), s.day,
		        s.fines_issued, s.fines_cancelled, s.fines_total::text,
		        s.unique_plates, s.anomaly_score
		   FROM officer_daily_stats s
		   LEFT JOIN users u ON u.id = s.officer_id
		  WHERE s.tenant_id = $1
		    AND s.day >= (now() AT TIME ZONE 'UTC')::date - ($2::int * INTERVAL '1 day')
		  ORDER BY s.anomaly_score DESC NULLS LAST, s.day DESC`,
		c.TenantID, days)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()

	type item struct {
		OfficerID      uuid.UUID `json:"officer_id"`
		OfficerName    string    `json:"officer_name"`
		Day            time.Time `json:"day"`
		FinesIssued    int       `json:"fines_issued"`
		FinesCancelled int       `json:"fines_cancelled"`
		FinesTotal     string    `json:"fines_total"`
		UniquePlates   int       `json:"unique_plates"`
		AnomalyScore   *float32  `json:"anomaly_score"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.OfficerID, &it.OfficerName, &it.Day,
			&it.FinesIssued, &it.FinesCancelled, &it.FinesTotal,
			&it.UniquePlates, &it.AnomalyScore); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":  out,
		"window": days,
	})
}

// rebuildStats triggers the rollup synchronously. Useful for tests, the
// CI smoke, and an admin "refresh now" button when fresh data is
// expected after a backfill.
func (a *API) rebuildStats(w http.ResponseWriter, r *http.Request) {
	if a.rollup == nil {
		httpx.WriteErr(w, httpx.Err(http.StatusServiceUnavailable, "no_rollup", "rollup job not wired"))
		return
	}
	if err := a.rollup.RunOnce(r.Context()); err != nil {
		httpx.WriteErr(w, httpx.Err(http.StatusInternalServerError, "rollup_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
