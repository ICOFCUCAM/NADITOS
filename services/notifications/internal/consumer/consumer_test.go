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

// drainOnce drives the consumer synchronously until the offset has
// caught up to the given event id, or fails. Synchronous-tick avoids
// the goroutine leak that bit us when one test's consumer kept
// processing events that the next test wrote.
func drainOnce(t *testing.T, env *testkit.Env, c *consumer.Consumer, throughEventID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		if err := c.Tick(ctx); err != nil {
			t.Fatalf("consumer Tick: %v", err)
		}
		var off int64
		_ = env.QueryRow(`SELECT last_event_id FROM event_consumer_offsets WHERE consumer='notifications'`).
			Scan(&off)
		if off >= throughEventID {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("consumer did not advance past %d in time", throughEventID)
}

// TestConsumer_FineIssued_SendsNotification: an emit of fine.issued
// produces a notification_record AND calls the Sender.
func TestConsumer_FineIssued_SendsNotification(t *testing.T) {
	env := testkit.Setup(t)
	vehicleID, email := seedVehicleAndCitizen(t, env)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)

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

// TestConsumer_FineCancelled_SendsNotification: when an admin
// resolves a dispute in the citizen's favour, the fines service
// emits fine.cancelled with the dispute reason; the consumer
// renders the "fine cancelled" message addressed to the vehicle's
// owner.
func TestConsumer_FineCancelled_SendsNotification(t *testing.T) {
	env := testkit.Setup(t)
	vehicleID, email := seedVehicleAndCitizen(t, env)

	// Seed an officer (foreign key target for fines.issued_by) and a
	// fine row so the renderer's vehicle lookup works.
	officerID := uuid.New()
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	          VALUES ($1, $2, $3, '!', 'Test officer')`,
		officerID, env.Tenant, "officer-"+officerID.String()[:8]+"@x")
	fineID := uuid.New()
	env.Exec(`INSERT INTO fines
	            (id, tenant_id, vehicle_id, plate, offence_code,
	             amount, currency, status, due_at, issued_by)
	          VALUES ($1, $2, $3, 'NOTIF-CXL', 'INS_EXPIRED',
	                  400, 'EUR', 'cancelled'::fine_status,
	                  now() + interval '14 days', $4)`,
		fineID, env.Tenant, vehicleID, officerID)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)
	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeFineCancelled, Version: 1,
		Source: "fines", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		Data: events.FineCancelledPayload{
			FineID: fineID.String(),
			Reason: "dispute upheld: plate misread",
		},
	})
	drainOnce(t, env, c, eid)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.out) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.out))
	}
	if sender.out[0].To != email {
		t.Fatalf("recipient: want %s got %s", email, sender.out[0].To)
	}
	if !strings.Contains(sender.out[0].Body, "cancelled") ||
		!strings.Contains(sender.out[0].Body, "plate misread") {
		t.Fatalf("body missing cancellation reason: %q", sender.out[0].Body)
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
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)
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

// seedBuyerOwner seeds a citizen user + their owners row + a vehicle
// pointing at that owner. Returns (vehicle_id, plate, ownerID, email)
// — the renderer test uses ownerID as the to_owner of the transfer
// event and asserts the email gets the message.
func seedBuyerOwner(t *testing.T, env *testkit.Env) (string, string, string, string) {
	t.Helper()
	uid := uuid.New()
	email := fmt.Sprintf("%s@%s", uid.String()[:8], env.Tenant)
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	         VALUES ($1, $2, $3, '!', 'New owner')`,
		uid, env.Tenant, email)
	ownerID := uuid.New()
	env.Exec(`INSERT INTO owners (id, tenant_id, user_id, full_name)
	         VALUES ($1, $2, $3::uuid, 'New owner')`,
		ownerID, env.Tenant, uid)
	vid := uuid.New()
	plate := "XFR-" + uid.String()[:6]
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate, owner_id)
	         VALUES ($1, $2, $3, $4)`, vid, env.Tenant, plate, ownerID)
	return vid.String(), plate, ownerID.String(), email
}

// TestConsumer_LicenseDemerit_SendsRunningTotal: on every demerit
// event the citizen gets a notice with how many points they took
// and where they sit against the threshold. Lets them course-correct
// before they hit suspension.
func TestConsumer_LicenseDemerit_SendsRunningTotal(t *testing.T) {
	env := testkit.Setup(t)
	lid, email := seedLicenseAndCitizen(t, env)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)
	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeLicenseDemerit, Version: 1,
		Source: "license", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		Data: events.LicenseDemeritPayload{
			LicenseID: lid, Delta: 6, Reason: "fine:SPEED_30",
			Source: "fine", NewTotal: 9,
		},
	})
	drainOnce(t, env, c, eid)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.out) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.out))
	}
	if sender.out[0].To != email {
		t.Fatalf("recipient: want %s got %s", email, sender.out[0].To)
	}
	if !strings.Contains(sender.out[0].Body, "+6 demerit") &&
		!strings.Contains(sender.out[0].Body, "6 demerit point") {
		t.Fatalf("body missing delta: %q", sender.out[0].Body)
	}
	if !strings.Contains(sender.out[0].Body, "9") {
		t.Fatalf("body missing new total: %q", sender.out[0].Body)
	}
	// Threshold default is 12, so 12 - 9 = 3 remaining — must surface.
	if !strings.Contains(sender.out[0].Body, "3") {
		t.Fatalf("body missing remaining-to-suspension: %q", sender.out[0].Body)
	}
}

// TestConsumer_VehicleTransferred_NotifiesBuyer: when an accepted
// transfer's outbox event arrives, the new owner gets one
// "vehicle transferred to you" notification at the address resolved
// from owners → users. The seller is not notified.
func TestConsumer_VehicleTransferred_NotifiesBuyer(t *testing.T) {
	env := testkit.Setup(t)
	vid, plate, buyerOwnerID, buyerEmail := seedBuyerOwner(t, env)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)
	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeVehicleTransferred, Version: 1,
		Source: "registry", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		Data: events.VehicleTransferredPayload{
			VehicleID: vid, Plate: plate,
			FromOwner: uuid.NewString(), // any UUID — we don't notify the seller
			ToOwner:   buyerOwnerID,
		},
	})
	drainOnce(t, env, c, eid)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.out) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.out))
	}
	if sender.out[0].To != buyerEmail {
		t.Fatalf("recipient: want %s got %s", buyerEmail, sender.out[0].To)
	}
	if !strings.Contains(sender.out[0].Body, plate) {
		t.Fatalf("body missing plate: %q", sender.out[0].Body)
	}
}

// seedLicenseAndCitizen seeds a driver license linked to a citizen user
// and returns (licenseID, citizenEmail).
func seedLicenseAndCitizen(t *testing.T, env *testkit.Env) (string, string) {
	t.Helper()
	uid := uuid.New()
	email := fmt.Sprintf("%s@%s", uid.String()[:8], env.Tenant)
	env.Exec(`INSERT INTO users (id, tenant_id, email, password_hash, full_name)
	         VALUES ($1, $2, $3, '!', 'Test Driver')`,
		uid, env.Tenant, email)

	lid := uuid.New()
	env.Exec(`INSERT INTO driver_licenses (id, tenant_id, user_id, license_number,
	             full_name, classes, issued_at, expires_at)
	         VALUES ($1, $2, $3, $4, 'Test Driver', $5, '2020-01-01', '2030-01-01')`,
		lid, env.Tenant, uid, "DL-"+lid.String()[:6], []string{"B"})
	return lid.String(), email
}

// TestConsumer_LicenseSuspended_SendsNotification: emit license.suspended
// → consumer renders the message → dev-stub sender called.
func TestConsumer_LicenseSuspended_SendsNotification(t *testing.T) {
	env := testkit.Setup(t)
	lid, email := seedLicenseAndCitizen(t, env)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)
	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeLicenseSuspended, Version: 1,
		Source: "license", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		Data: events.LicenseSuspendedPayload{
			LicenseID: lid, Reason: "demerit threshold reached",
			TriggerKind: "demerit",
			StartsAt:    "2026-05-01T00:00:00Z",
			EndsAt:      "2026-11-01T00:00:00Z",
		},
	})
	drainOnce(t, env, c, eid)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.out) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.out))
	}
	if sender.out[0].To != email {
		t.Fatalf("recipient: want %s got %s", email, sender.out[0].To)
	}
	if !strings.Contains(sender.out[0].Subject, "suspended") {
		t.Fatalf("subject: %q", sender.out[0].Subject)
	}
	if !strings.Contains(sender.out[0].Body, "demerit threshold reached") {
		t.Fatalf("body missing reason: %q", sender.out[0].Body)
	}
}

// TestConsumer_LicenseReinstated_SendsNotification: closes the demerit
// loop. The lift handler emits license.reinstated; the citizen gets a
// "you can drive again" message.
func TestConsumer_LicenseReinstated_SendsNotification(t *testing.T) {
	env := testkit.Setup(t)
	lid, email := seedLicenseAndCitizen(t, env)

	sender := &captureSender{}
	c := consumer.New(env.AdminPool(), captureLogger(t), sender)
	eid := writeOutbox(t, env, events.Envelope{
		ID: uuid.NewString(), Type: events.TypeLicenseReinstated, Version: 1,
		Source: "license", TenantID: env.Tenant, OccurredAt: time.Now().UTC(),
		Data: events.LicenseReinstatedPayload{
			LicenseID:    lid,
			SuspensionID: uuid.NewString(),
		},
	})
	drainOnce(t, env, c, eid)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.out) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.out))
	}
	if sender.out[0].To != email {
		t.Fatalf("recipient: want %s got %s", email, sender.out[0].To)
	}
	if !strings.Contains(sender.out[0].Subject, "reinstated") {
		t.Fatalf("subject: %q", sender.out[0].Subject)
	}

	// notification_records row exists with the correct template.
	var template string
	if err := env.QueryRow(
		`SELECT template FROM notification_records
		   WHERE tenant_id=$1 AND related_event=$2`,
		env.Tenant, eid).Scan(&template); err != nil {
		t.Fatal(err)
	}
	if template != "license.reinstated.v1" {
		t.Fatalf("template: %s", template)
	}
}
