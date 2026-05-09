package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/auth"
	"github.com/icofcucam/naditos/packages/go-common/contracts"
	"github.com/icofcucam/naditos/packages/go-common/contracts/notifications"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/notifications/internal/api"
)

// issueFor signs a JWT for an existing user id (we can't use
// env.Token because that creates a fresh user each call; the inbox
// tests need to mint a token for a user we've already seeded with
// notifications addressed to their email).
func issueFor(t *testing.T, env *testkit.Env, userID, role string) string {
	t.Helper()
	uid, err := uuid.Parse(userID)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := env.Issuer.Sign(uid, auth.Claims{
		TenantID: env.Tenant, Role: role,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// noopSender is a Sender that always succeeds — the inbox tests
// don't exercise the send path, just the read surface.
type noopSender struct{}

func (noopSender) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "notifications", Provider: "noop"}
}
func (noopSender) Send(_ context.Context, _ notifications.Message) (*notifications.Receipt, error) {
	return &notifications.Receipt{ID: "stub", Status: "sent", Provider: "noop"}, nil
}

func build(env *testkit.Env) http.Handler {
	return api.New(env.Cfg, discardLogger(), env.Pool, env.Issuer, noopSender{})
}

// seedCitizenWithEmail creates a citizen user + owners row with the
// given email/phone and inserts notification_records for them.
func seedCitizenWithEmailAndNotifs(t *testing.T, env *testkit.Env, email, phone string,
	mine int, others int) string {
	t.Helper()
	uid := uuid.NewString()
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	          VALUES ($1::uuid, $2, $3, '!', 'Test')`,
		uid, env.Tenant, email)
	env.Exec(`INSERT INTO owners (tenant_id, user_id, full_name, phone)
	          VALUES ($1, $2::uuid, 'Test', NULLIF($3,''))`,
		env.Tenant, uid, phone)
	for i := 0; i < mine; i++ {
		env.Exec(`INSERT INTO notification_records
		            (tenant_id, channel, recipient, body, template, status, sent_at)
		          VALUES ($1, 'email', $2, $3, 'fine.issued.v1', 'sent', now())`,
			env.Tenant, email, fmt.Sprintf("body-%d", i))
	}
	for i := 0; i < others; i++ {
		env.Exec(`INSERT INTO notification_records
		            (tenant_id, channel, recipient, body, template, status, sent_at)
		          VALUES ($1, 'email', 'someone-else@example.com', $2, 'fine.issued.v1', 'sent', now())`,
			env.Tenant, fmt.Sprintf("body-%d", i))
	}
	// Also drop in one suppressed row addressed to me — must NOT
	// appear in the inbox; the inbox is "sent" only.
	env.Exec(`INSERT INTO notification_records
	            (tenant_id, channel, recipient, body, status)
	          VALUES ($1, 'email', $2, 'suppressed-body', 'suppressed')`,
		env.Tenant, email)
	return uid
}

// TestMyInbox_ReturnsOnlyMine: a citizen's inbox shows their own
// sent messages and never another recipient's. Tenant-scoped.
func TestMyInbox_ReturnsOnlyMine(t *testing.T) {
	env := testkit.Setup(t)
	email := uuid.NewString()[:8] + "@example.com"
	uid := seedCitizenWithEmailAndNotifs(t, env, email, "", 3, 2)

	tok := issueFor(t, env, uid, "citizen")
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/notifications", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("inbox: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			Recipient string `json:"recipient"`
			Status    string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 3 {
		t.Fatalf("want 3 items (mine), got %d", len(out.Items))
	}
	for _, it := range out.Items {
		if it.Recipient != email {
			t.Errorf("recipient leak: %s", it.Recipient)
		}
		if it.Status != "sent" {
			t.Errorf("non-sent in inbox: %s", it.Status)
		}
	}
}

// TestList_AdminEnvelopeShape pins the {items:[{id, channel, recipient,
// status, provider, ...}]} shape that the admin /notifications page
// consumes. A flatten or rename here would silently break the page.
func TestList_AdminEnvelopeShape(t *testing.T) {
	env := testkit.Setup(t)
	tok, _ := env.Token("admin")
	// Two records in different statuses so the response isn't trivially
	// homogeneous.
	env.Exec(`INSERT INTO notification_records
	            (tenant_id, channel, recipient, body, template, status,
	             provider, provider_ref, sent_at)
	          VALUES ($1, 'email', 'a@example.com', 'b', 'fine.issued.v1',
	                  'sent', 'noop', 'ref-1', now())`, env.Tenant)
	env.Exec(`INSERT INTO notification_records
	            (tenant_id, channel, recipient, body, status)
	          VALUES ($1, 'sms', '+1', 'b', 'failed')`, env.Tenant)

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/notify", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			ID          string  `json:"id"`
			Channel     string  `json:"channel"`
			Recipient   string  `json:"recipient"`
			Subject     string  `json:"subject"`
			Template    *string `json:"template"`
			Status      string  `json:"status"`
			Provider    string  `json:"provider"`
			ProviderRef string  `json:"provider_ref"`
			CreatedAt   string  `json:"created_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) < 2 {
		t.Fatalf("want >=2 items, got %d", len(out.Items))
	}
	// Find the failed-sms row and the sent-email row.
	var sawSent, sawFailed bool
	for _, it := range out.Items {
		if it.ID == "" || it.Channel == "" || it.Recipient == "" || it.Status == "" {
			t.Errorf("item missing required fields: %+v", it)
		}
		if it.Status == "sent" && it.Provider == "noop" && it.ProviderRef == "ref-1" {
			sawSent = true
		}
		if it.Status == "failed" {
			sawFailed = true
		}
	}
	if !sawSent {
		t.Error("sent row with provider/provider_ref not surfaced")
	}
	if !sawFailed {
		t.Error("failed row not surfaced — admin would never see delivery problems")
	}
}

// TestMyInbox_PhoneAlsoMatches: a citizen with phone but no email
// (or whose notifications went via SMS) sees them in the inbox too.
func TestMyInbox_PhoneAlsoMatches(t *testing.T) {
	env := testkit.Setup(t)
	uid := uuid.NewString()
	email := "phone-citizen-" + uid[:8] + "@example.com"
	phone := "+1-555-" + uid[:4]
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	          VALUES ($1::uuid, $2, $3, '!', 'Phone Person')`,
		uid, env.Tenant, email)
	env.Exec(`INSERT INTO owners (tenant_id, user_id, full_name, phone)
	          VALUES ($1, $2::uuid, 'Phone Person', $3)`,
		env.Tenant, uid, phone)
	env.Exec(`INSERT INTO notification_records
	            (tenant_id, channel, recipient, body, status, sent_at)
	          VALUES ($1, 'sms', $2, 'sms-body', 'sent', now())`,
		env.Tenant, phone)

	tok := issueFor(t, env, uid, "citizen")
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/notifications", "", tok))
	if rec.Code != http.StatusOK {
		t.Fatalf("inbox: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct{ Channel string `json:"channel"` } `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].Channel != "sms" {
		t.Fatalf("phone-matched inbox: %+v", out.Items)
	}
}
