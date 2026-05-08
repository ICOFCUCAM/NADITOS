// Package api implements the append-only, hash-chained audit log.
//
// Hash chain construction:
//   row.hash = SHA-256( prev_hash || canonicalJSON(row) )
//
// The previous row is selected for UPDATE per tenant to serialize writes
// inside a transaction. Database triggers reject UPDATE/DELETE.
package api

import (
	"crypto/sha256"
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

// Rollup is the part of the rollup.Job the audit API needs — kept as a
// minimal interface so the api package doesn't import the rollup
// package's full surface.
type Rollup interface {
	RunOnce(ctx context.Context) error
}

type API struct {
	cfg    config.Service
	log    *slog.Logger
	pool   *pgxpool.Pool
	issuer *auth.Issuer
	rollup Rollup
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool,
	issuer *auth.Issuer, roll Rollup) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, issuer: issuer, rollup: roll}
	mux := http.NewServeMux()
	// audit log
	mux.HandleFunc("POST /v1/audit/events", a.write)
	mux.HandleFunc("GET  /v1/audit/events", a.list)
	mux.HandleFunc("GET  /v1/audit/verify", a.verify)
	// officer analytics — admin only
	mux.Handle("GET  /v1/audit/officers/stats",
		issuer.Middleware(auth.RequirePermission("audit:read")(http.HandlerFunc(a.officerStats))))
	mux.Handle("POST /v1/audit/officers/stats:rebuild",
		issuer.Middleware(auth.RequirePermission("audit:read")(http.HandlerFunc(a.rebuildStats))))
	return mux
}

type event struct {
	OccurredAt   time.Time       `json:"occurred_at"`
	TenantID     string          `json:"tenant_id"`
	ActorUser    string          `json:"actor_user,omitempty"`
	ActorRole    string          `json:"actor_role,omitempty"`
	ActorDevice  string          `json:"actor_device,omitempty"`
	ActorIP      string          `json:"actor_ip,omitempty"`
	Service      string          `json:"service"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Before       json.RawMessage `json:"before,omitempty"`
	After        json.RawMessage `json:"after,omitempty"`
}

func (a *API) write(w http.ResponseWriter, r *http.Request) {
	var ev event
	if err := httpx.ReadJSON(r, &ev); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if ev.TenantID == "" || ev.Action == "" || ev.Service == "" || ev.ResourceType == "" {
		httpx.WriteErr(w, httpx.ErrBadRequest)
		return
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.ActorIP == "" {
		ev.ActorIP = clientIP(r)
	}

	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer tx.Rollback(ctx)

	// audit_events has RLS enabled; this service writes for any tenant.
	if _, err := tx.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		httpx.WriteErr(w, err)
		return
	}

	var prev []byte
	_ = tx.QueryRow(ctx,
		`SELECT hash FROM audit_events WHERE tenant_id=$1
		   ORDER BY id DESC LIMIT 1 FOR UPDATE`, ev.TenantID).Scan(&prev)

	canon, err := canonical(ev)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	h := sha256.New()
	if prev != nil {
		h.Write(prev)
	}
	h.Write(canon)
	hash := h.Sum(nil)

	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO audit_events (
			tenant_id, occurred_at,
			actor_user, actor_role, actor_device, actor_ip,
			service, action, resource_type, resource_id,
			before, after, prev_hash, hash)
		 VALUES (
			$1,$2,
			NULLIF($3,'')::uuid, NULLIF($4,''), NULLIF($5,''), NULLIF($6,'')::inet,
			$7,$8,$9,$10,
			$11,$12,$13,$14)
		 RETURNING id`,
		ev.TenantID, ev.OccurredAt,
		ev.ActorUser, ev.ActorRole, ev.ActorDevice, ev.ActorIP,
		ev.Service, ev.Action, ev.ResourceType, ev.ResourceID,
		nullJSON(ev.Before), nullJSON(ev.After), prev, hash,
	).Scan(&id)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id":   id,
		"hash": hex.EncodeToString(hash),
	})
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		tenant = r.Header.Get("X-Tenant-Id")
	}
	if tenant == "" {
		httpx.WriteErr(w, httpx.Err(400, "tenant_required", "tenant_id is required"))
		return
	}
	conn, err := a.pool.Acquire(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	_, _ = conn.Exec(r.Context(), "SET LOCAL row_security = off")

	rows, err := conn.Query(r.Context(),
		`SELECT id, occurred_at, actor_user::text, actor_role, actor_device,
		        host(actor_ip), service, action, resource_type, resource_id,
		        before, after, encode(hash,'hex')
		   FROM audit_events
		  WHERE tenant_id=$1
		  ORDER BY id DESC LIMIT 200`, tenant)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()

	type item struct {
		ID           int64           `json:"id"`
		OccurredAt   time.Time       `json:"occurred_at"`
		ActorUser    *string         `json:"actor_user"`
		ActorRole    *string         `json:"actor_role"`
		ActorDevice  *string         `json:"actor_device"`
		ActorIP      *string         `json:"actor_ip"`
		Service      string          `json:"service"`
		Action       string          `json:"action"`
		ResourceType string          `json:"resource_type"`
		ResourceID   *string         `json:"resource_id"`
		Before       json.RawMessage `json:"before"`
		After        json.RawMessage `json:"after"`
		Hash         string          `json:"hash"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.OccurredAt, &it.ActorUser, &it.ActorRole,
			&it.ActorDevice, &it.ActorIP, &it.Service, &it.Action,
			&it.ResourceType, &it.ResourceID, &it.Before, &it.After, &it.Hash); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// verify walks the chain and reports the first break, if any.
func (a *API) verify(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		httpx.WriteErr(w, httpx.Err(400, "tenant_required", "tenant_id is required"))
		return
	}
	conn, err := a.pool.Acquire(r.Context())
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer conn.Release()
	_, _ = conn.Exec(r.Context(), "SET LOCAL row_security = off")

	rows, err := conn.Query(r.Context(),
		`SELECT id, occurred_at, tenant_id,
		        COALESCE(actor_user::text,''), COALESCE(actor_role,''),
		        COALESCE(actor_device,''), COALESCE(host(actor_ip),''),
		        service, action, resource_type, COALESCE(resource_id,''),
		        before, after, prev_hash, hash
		   FROM audit_events
		  WHERE tenant_id=$1
		  ORDER BY id ASC`, tenant)
	if err != nil {
		httpx.WriteErr(w, err)
		return
	}
	defer rows.Close()

	checked := 0
	var prev []byte
	for rows.Next() {
		var (
			id                                       int64
			ts                                       time.Time
			tid, au, ar, ad, ai, sv, ac, rt, ri      string
			before, after                            json.RawMessage
			storedPrev, storedHash                   []byte
		)
		if err := rows.Scan(&id, &ts, &tid, &au, &ar, &ad, &ai,
			&sv, &ac, &rt, &ri, &before, &after, &storedPrev, &storedHash); err != nil {
			httpx.WriteErr(w, err)
			return
		}
		ev := event{
			OccurredAt: ts.UTC(), TenantID: tid,
			ActorUser: au, ActorRole: ar, ActorDevice: ad, ActorIP: ai,
			Service: sv, Action: ac, ResourceType: rt, ResourceID: ri,
			Before: before, After: after,
		}
		canon, _ := canonical(ev)
		h := sha256.New()
		if prev != nil {
			h.Write(prev)
		}
		h.Write(canon)
		got := h.Sum(nil)
		if string(got) != string(storedHash) || string(storedPrev) != string(prev) {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"ok":           false,
				"broken_at":    id,
				"checked":      checked,
			})
			return
		}
		prev = storedHash
		checked++
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "checked": checked})
}

// canonical produces a stable byte representation of an event.
//
// The hash chain must survive a JSONB round-trip on `before` and `after`
// — Postgres normalizes JSONB on insert (alphabetical keys, no
// whitespace, canonical numbers) so the bytes the verifier reads are
// NOT the bytes the writer received from the wire. We renormalize both
// fields through Go's json package on both write and verify so the hash
// is computed against the same canonical form.
func canonical(ev event) ([]byte, error) {
	ev.OccurredAt = ev.OccurredAt.UTC().Truncate(time.Millisecond)
	ev.Before = normalizeJSON(ev.Before)
	ev.After = normalizeJSON(ev.After)
	return json.Marshal(ev)
}

func normalizeJSON(b json.RawMessage) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return b // malformed JSON: leave as-is so mismatch signals tamper
	}
	out, err := json.Marshal(v)
	if err != nil {
		return b
	}
	return out
}

func nullJSON(b json.RawMessage) any {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return string(b)
}

func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if i := strings.Index(h, ","); i > 0 {
			return strings.TrimSpace(h[:i])
		}
		return strings.TrimSpace(h)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}
