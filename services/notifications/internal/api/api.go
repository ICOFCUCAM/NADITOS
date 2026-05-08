// Package api wires the notifications service's HTTP surface.
//
// This is mostly admin / operator-facing — most outbound messages flow
// through the consumer that drains event_outbox. The HTTP API is here
// so admins can send a one-off message and citizens can read their
// notification history.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/config"
	"github.com/icofcucam/naditos/packages/go-common/contracts/notifications"
	"github.com/icofcucam/naditos/packages/go-common/db"
	"github.com/icofcucam/naditos/packages/go-common/httpx"
)

type API struct {
	cfg    config.Service
	log    *slog.Logger
	pool   *pgxpool.Pool
	issuer *auth.Issuer
	send   notifications.Sender
}

func New(cfg config.Service, log *slog.Logger, pool *pgxpool.Pool,
	issuer *auth.Issuer, sender notifications.Sender) http.Handler {
	a := &API{cfg: cfg, log: log, pool: pool, issuer: issuer, send: sender}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/notify",
		issuer.Middleware(http.HandlerFunc(a.notify)))
	mux.Handle("GET /v1/notify",
		issuer.Middleware(http.HandlerFunc(a.list)))
	return mux
}

// POST /v1/notify — admin ad-hoc send. Records and sends.
func (a *API) notify(w http.ResponseWriter, r *http.Request) {
	type req struct {
		Channel string `json:"channel"` // sms|email|push
		To      string `json:"to"`
		Subject string `json:"subject,omitempty"`
		Body    string `json:"body"`
	}
	var in req
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.WriteErr(w, err)
		return
	}
	if in.Channel == "" || in.To == "" || in.Body == "" {
		httpx.WriteErr(w, httpx.Err(400, "missing", "channel, to, and body are required"))
		return
	}

	c := auth.ClaimsFrom(r.Context())
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()

	var id uuid.UUID
	if err := conn.QueryRow(r.Context(),
		`INSERT INTO notification_records
		   (tenant_id, channel, recipient, subject, body, status)
		 VALUES ($1, $2, $3, NULLIF($4,''), $5, 'pending')
		 RETURNING id`,
		c.TenantID, in.Channel, in.To, in.Subject, in.Body).Scan(&id); err != nil {
		httpx.WriteErr(w, err); return
	}

	receipt, err := a.send.Send(r.Context(), notifications.Message{
		TenantID: c.TenantID,
		Channel:  notifications.Channel(in.Channel),
		To:       in.To, Subject: in.Subject, Body: in.Body,
	})
	if err != nil {
		_, _ = conn.Exec(r.Context(),
			`UPDATE notification_records SET status='failed', last_error=$2, attempts=attempts+1
			   WHERE id=$1`, id, err.Error())
		httpx.WriteErr(w, httpx.Err(502, "send_failed", err.Error()))
		return
	}
	_, _ = conn.Exec(r.Context(),
		`UPDATE notification_records
		    SET status='sent', sent_at=now(),
		        provider=$2, provider_ref=$3, attempts=attempts+1
		  WHERE id=$1`, id, a.send.Info().Provider, receipt.ID)

	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"id":           id,
		"status":       "sent",
		"provider":     a.send.Info().Provider,
		"provider_ref": receipt.ID,
	})
}

// GET /v1/notify — recent history for the current tenant.
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	conn, err := db.WithTenant(r.Context(), a.pool)
	if err != nil { httpx.WriteErr(w, err); return }
	defer conn.Release()

	rows, err := conn.Query(r.Context(),
		`SELECT id, channel, recipient, COALESCE(subject,''), template,
		        status::text, COALESCE(provider,''), COALESCE(provider_ref,''),
		        created_at, sent_at
		   FROM notification_records
		  ORDER BY created_at DESC LIMIT 200`)
	if err != nil { httpx.WriteErr(w, err); return }
	defer rows.Close()

	type item struct {
		ID          uuid.UUID  `json:"id"`
		Channel     string     `json:"channel"`
		Recipient   string     `json:"recipient"`
		Subject     string     `json:"subject"`
		Template    *string    `json:"template"`
		Status      string     `json:"status"`
		Provider    string     `json:"provider"`
		ProviderRef string     `json:"provider_ref"`
		CreatedAt   time.Time  `json:"created_at"`
		SentAt      *time.Time `json:"sent_at"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		_ = rows.Scan(&it.ID, &it.Channel, &it.Recipient, &it.Subject, &it.Template,
			&it.Status, &it.Provider, &it.ProviderRef, &it.CreatedAt, &it.SentAt)
		out = append(out, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}
