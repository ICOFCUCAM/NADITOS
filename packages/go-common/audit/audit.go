// Package audit emits append-only audit events to the audit service.
//
// Every state-changing handler should call audit.Emit(ctx, ...) before
// returning success. Failures to emit are logged but do not block the
// request — the audit service has its own durability guarantees.
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/auth"
)

type Event struct {
	OccurredAt   time.Time      `json:"occurred_at"`
	TenantID     string         `json:"tenant_id"`
	ActorUser    string         `json:"actor_user,omitempty"`
	ActorRole    string         `json:"actor_role,omitempty"`
	ActorDevice  string         `json:"actor_device,omitempty"`
	ActorIP      string         `json:"actor_ip,omitempty"`
	Service      string         `json:"service"`
	Action       string         `json:"action"`         // e.g. "fine.create"
	ResourceType string         `json:"resource_type"`  // e.g. "fine"
	ResourceID   string         `json:"resource_id"`
	Before       any            `json:"before,omitempty"`
	After        any            `json:"after,omitempty"`
}

type Client struct {
	BaseURL string
	Service string
	HTTP    *http.Client
}

func New(baseURL, service string) *Client {
	return &Client{
		BaseURL: baseURL,
		Service: service,
		HTTP:    &http.Client{Timeout: 3 * time.Second},
	}
}

// Emit fires-and-mostly-forgets an audit event. Returns the error so callers
// can log it; never blocks the user-facing flow on audit failures.
func (c *Client) Emit(ctx context.Context, action, resourceType, resourceID string, before, after any) error {
	if c == nil || c.BaseURL == "" {
		return nil
	}
	cl := auth.ClaimsFrom(ctx)
	ev := Event{
		OccurredAt:   time.Now().UTC(),
		Service:      c.Service,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Before:       before,
		After:        after,
	}
	if cl != nil {
		ev.TenantID = cl.TenantID
		ev.ActorUser = cl.Subject
		ev.ActorRole = cl.Role
		ev.ActorDevice = cl.DeviceID
	}
	return c.EmitRaw(ctx, &ev)
}

// EmitRaw posts a fully-constructed event. Use this when the caller
// can't rely on auth.Claims being in the context — most notably the
// auth service itself, where login.success and login.failed need to
// be recorded *before* a JWT exists. The caller is responsible for
// populating TenantID, ActorUser, etc.
//
// The OccurredAt and Service fields are filled in here if the caller
// left them zero, so a minimal call site only needs to set Action,
// ResourceType, ResourceID, TenantID, and Before/After.
func (c *Client) EmitRaw(ctx context.Context, ev *Event) error {
	if c == nil || c.BaseURL == "" {
		return nil
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.Service == "" {
		ev.Service = c.Service
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/audit/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &httpError{Status: resp.StatusCode}
	}
	return nil
}

type httpError struct{ Status int }

func (e *httpError) Error() string { return "audit emit failed" }
