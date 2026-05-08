package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
	"github.com/icofcucam/naditos/packages/go-common/contracts/notifications"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/notifications/internal/consumer"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// captureLogger emits everything to a buffer that the test prints on
// failure. Use this when debugging consumer-handler errors.
func captureLogger(t *testing.T) *slog.Logger {
	buf := &strings.Builder{}
	t.Cleanup(func() {
		if t.Failed() && buf.Len() > 0 {
			t.Logf("consumer logs:\n%s", buf.String())
		}
	})
	return slog.New(slog.NewTextHandler(textBufWriter{buf}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type textBufWriter struct{ b *strings.Builder }

func (w textBufWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// captureSender implements notifications.Sender and records every call.
type captureSender struct {
	mu  sync.Mutex
	out []notifications.Message
	err error
}

func (s *captureSender) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "notifications", Provider: "capture"}
}
func (s *captureSender) Send(_ context.Context, m notifications.Message) (*notifications.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	s.out = append(s.out, m)
	return &notifications.Receipt{ID: uuid.NewString(), Status: "sent", Provider: "capture"}, nil
}

func writeOutbox(t *testing.T, env *testkit.Env, body events.Envelope) int64 {
	t.Helper()
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := env.QueryRow(
		`INSERT INTO event_outbox (tenant_id, envelope) VALUES ($1, $2) RETURNING id`,
		body.TenantID, bodyJSON).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// seedVehicleAndCitizen creates a citizen user with a known email,
// links them to an owners row, and registers a vehicle they own.
// Returns (vehicleID, citizenEmail).
func seedVehicleAndCitizen(t *testing.T, env *testkit.Env) (string, string) {
	t.Helper()
	uid := uuid.New()
	email := fmt.Sprintf("%s@%s", uid.String()[:8], env.Tenant)
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	         VALUES ($1, $2, $3, '!', 'Test Citizen')`,
		uid, env.Tenant, email)

	ownerID := uuid.New()
	env.Exec(`INSERT INTO owners (id, tenant_id, user_id, full_name, email)
	         VALUES ($1, $2, $3, 'Test Citizen', $4)`,
		ownerID, env.Tenant, uid, email)

	vehicleID := uuid.New()
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate, owner_id)
	         VALUES ($1, $2, 'NOTIF-1', $3)`,
		vehicleID, env.Tenant, ownerID)

	return vehicleID.String(), email
}

// drainOnce waits until the consumer's offset has caught up to the
// given event id (or the test deadline trips).
func drainOnce(t *testing.T, env *testkit.Env, c *consumer.Consumer, throughEventID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go c.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var off int64
		_ = env.QueryRow(`SELECT last_event_id FROM event_consumer_offsets WHERE consumer='notifications'`).
			Scan(&off)
		if off >= throughEventID {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("consumer did not advance past %d in time", throughEventID)
}

// TestConsumer_FineIssued_SendsNotification: an emit of fine.issued
// produces a notification_record AND calls the Sender.
func TestConsumer_FineIssued_SendsNotification(t *testing.T) {
	env := testkit.Setup(t)
	vehicleID, email := seedVehicleAndCitizen(t, env)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), discardLogger(), sender)

	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeFineIssued, Version: 1,
		Source: "fines", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		ActorRole: "officer",
		Data: events.FineIssuedPayload{
			FineID: uuid.NewString(), Plate: "NOTIF-1", VehicleID: vehicleID,
			OffenceCode: "INS_EXPIRED", Amount: "400.00", Currency: "EUR",
		},
	})
	drainOnce(t, env, c, eid)

	// Sender was called with the citizen's email.
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.out) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.out))
	}
	if sender.out[0].To != email {
		t.Fatalf("recipient: want %s got %s", email, sender.out[0].To)
	}
	if !strings.Contains(sender.out[0].Body, "NOTIF-1") ||
		!strings.Contains(sender.out[0].Body, "400.00") {
		t.Fatalf("body missing plate/amount: %q", sender.out[0].Body)
	}

	// notification_records row exists with status=sent.
	var status, provider string
	if err := env.QueryRow(
		`SELECT status::text, COALESCE(provider,'') FROM notification_records
		   WHERE tenant_id=$1 AND related_event=$2`,
		env.Tenant, eid).Scan(&status, &provider); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || provider != "capture" {
		t.Fatalf("status=%s provider=%s, want sent/capture", status, provider)
	}
}

// TestConsumer_NoRecipient_Suppressed: a fine.issued event for a
// vehicle whose owner has no email or phone records the notification
// as 'suppressed' rather than calling the Sender.
func TestConsumer_NoRecipient_Suppressed(t *testing.T) {
	env := testkit.Setup(t)
	// Vehicle with no owner.
	vid := uuid.New()
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate) VALUES ($1, $2, 'ORPHAN-1')`,
		vid, env.Tenant)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)
	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeFineIssued, Version: 1,
		Source: "fines", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		Data: events.FineIssuedPayload{
			FineID: uuid.NewString(), Plate: "ORPHAN-1", VehicleID: vid.String(),
			OffenceCode: "INS_EXPIRED", Amount: "400.00", Currency: "EUR",
		},
	})
	drainOnce(t, env, c, eid)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.out) != 0 {
		t.Fatalf("expected NO send (no recipient), got %d", len(sender.out))
	}

	var status string
	if err := env.QueryRow(
		`SELECT status::text FROM notification_records
		   WHERE tenant_id=$1 AND related_event=$2`,
		env.Tenant, eid).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "suppressed" {
		t.Fatalf("want status=suppressed, got %s", status)
	}
}

// TestConsumer_AdvancesPastFailedSends: even when Sender errors, the
// consumer advances its offset and records the failure.
func TestConsumer_AdvancesPastFailedSends(t *testing.T) {
	env := testkit.Setup(t)
	vehicleID, _ := seedVehicleAndCitizen(t, env)

	sender := &captureSender{err: errors.New("provider down")}
	c := consumer.New(env.AdminPool(), discardLogger(), sender)
	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeFineIssued, Version: 1,
		Source: "fines", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		Data: events.FineIssuedPayload{
			FineID: uuid.NewString(), Plate: "NOTIF-1", VehicleID: vehicleID,
			OffenceCode: "INS_EXPIRED", Amount: "400.00", Currency: "EUR",
		},
	})
	drainOnce(t, env, c, eid)

	var status, lastErr string
	if err := env.QueryRow(
		`SELECT status::text, COALESCE(last_error,'') FROM notification_records
		   WHERE tenant_id=$1 AND related_event=$2`,
		env.Tenant, eid).Scan(&status, &lastErr); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(lastErr, "provider down") {
		t.Fatalf("want failed/provider down, got status=%s err=%s", status, lastErr)
	}
}
