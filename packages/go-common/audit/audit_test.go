package audit_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/audit"
	"github.com/icofcucam/naditos/packages/go-common/auth"
)

// TestEmit_NilBaseURL_NoOp: a Client with empty BaseURL (typical of
// tests) must short-circuit without making a network call. Lets test
// fixtures pass commonAudit.New("", "service") without faking a HTTP
// server.
func TestEmit_NilBaseURL_NoOp(t *testing.T) {
	c := audit.New("", "test-service")
	if err := c.Emit(context.Background(), "x.action", "x", "id1", nil, nil); err != nil {
		t.Fatalf("noop emit returned err: %v", err)
	}
}

// TestEmit_PostsToAuditService: a real BaseURL receives a POST to
// /v1/audit/events with the action / resource fields and the JWT
// claims propagated as actor_user / actor_role / tenant_id.
func TestEmit_PostsToAuditService(t *testing.T) {
	type recorded struct {
		path string
		body audit.Event
	}
	var got recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got.body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := audit.New(srv.URL, "fines")
	ctx := auth.WithClaims(context.Background(), &auth.Claims{
		TenantID:    "demo",
		Role:        "officer",
		DeviceID:    "dev-1",
	})
	ctx = auth.WithClaims(ctx, &auth.Claims{
		TenantID: "demo", Role: "officer", DeviceID: "dev-1",
	})
	if err := c.Emit(ctx, "fine.create", "fine", "f-1",
		nil, map[string]string{"plate": "AB-12-CD"}); err != nil {
		t.Fatal(err)
	}

	if got.path != "/v1/audit/events" {
		t.Fatalf("path: %q", got.path)
	}
	if got.body.Service != "fines" || got.body.Action != "fine.create" {
		t.Fatalf("body: %+v", got.body)
	}
	if got.body.ResourceType != "fine" || got.body.ResourceID != "f-1" {
		t.Fatalf("resource: %+v", got.body)
	}
	if got.body.TenantID != "demo" || got.body.ActorRole != "officer" {
		t.Fatalf("actor: tenant=%s role=%s", got.body.TenantID, got.body.ActorRole)
	}
	if got.body.ActorDevice != "dev-1" {
		t.Fatalf("device: %s", got.body.ActorDevice)
	}
}

// TestEmit_ServerError_Returns: a 5xx from the audit service surfaces
// as an error so callers can log it. We never block the user-facing
// flow, but the caller must be able to see the failure happened.
func TestEmit_ServerError_Returns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := audit.New(srv.URL, "svc")
	err := c.Emit(context.Background(), "a", "b", "c", nil, nil)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Fatalf("err message: %v", err)
	}
}

// TestEmit_NoClaims_StillSends: ctx without auth claims should still
// produce a payload (with empty tenant/actor) — service-to-service
// calls during boot don't carry JWTs but still want audit lineage.
func TestEmit_NoClaims_StillSends(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := audit.New(srv.URL, "svc")
	if err := c.Emit(context.Background(), "boot", "system", "1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("hits: %d", hits)
	}
}
