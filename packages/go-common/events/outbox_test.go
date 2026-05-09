package events_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
)

// TestWriteOutbox_PersistsEnvelope: a write inside a tx that commits
// produces an event_outbox row with the marshalled envelope and the
// tenant_id pulled from the envelope. This is the contract producers
// rely on.
func TestWriteOutbox_PersistsEnvelope(t *testing.T) {
	env := testkit.Setup(t)
	ctx := context.Background()

	tx, err := env.AdminPool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	envel := events.NewEnvelope("test-svc", env.Tenant, "test.event", 1,
		map[string]string{"k": "v"})
	if err := events.WriteOutbox(ctx, tx, envel); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	var body string
	if err := env.QueryRow(
		`SELECT count(*), max(envelope->>'type') FROM event_outbox
		  WHERE tenant_id=$1 AND envelope->>'type'='test.event'`, env.Tenant).
		Scan(&n, &body); err != nil {
		t.Fatal(err)
	}
	if n != 1 || body != "test.event" {
		t.Fatalf("want 1 row of type test.event, got n=%d body=%s", n, body)
	}
}

// TestWriteOutbox_RolledBackIsGone: a tx that rolls back leaves NO
// row behind. This is the whole point of using the outbox inside the
// caller's tx instead of publishing directly.
func TestWriteOutbox_RolledBackIsGone(t *testing.T) {
	env := testkit.Setup(t)
	ctx := context.Background()

	tx, err := env.AdminPool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	envel := events.NewEnvelope("rb-svc", env.Tenant, "rb.event", 1, nil)
	if err := events.WriteOutbox(ctx, tx, envel); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx) // explicit rollback

	var n int
	if err := env.QueryRow(
		`SELECT count(*) FROM event_outbox
		  WHERE tenant_id=$1 AND envelope->>'type'='rb.event'`, env.Tenant).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rollback left %d rows; want 0", n)
	}
}

// TestRelay_DrainsToBus: the relay reads undelivered rows, calls
// bus.Publish for each, marks them delivered. Once-only delivery on
// the happy path.
func TestRelay_DrainsToBus(t *testing.T) {
	env := testkit.Setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewInProc(discardLogger())
	var got int64
	_ = bus.Subscribe("relay.drain", func(_ context.Context, _ events.Envelope) error {
		atomic.AddInt64(&got, 1)
		return nil
	})

	// Seed three events.
	for i := 0; i < 3; i++ {
		tx, _ := env.AdminPool().Begin(ctx)
		_ = events.WriteOutbox(ctx, tx, events.NewEnvelope(
			"relay-test", env.Tenant, "relay.drain", 1, nil))
		_ = tx.Commit(ctx)
	}

	relay := events.NewRelay(env.AdminPool(), discardLogger(), bus)
	go relay.Run(ctx)

	// Wait up to 5s for the bus to see all three.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&got) >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if atomic.LoadInt64(&got) < 3 {
		t.Fatalf("relay drained only %d/3", got)
	}

	// All seeded rows have delivered_at set.
	var undelivered int
	_ = env.QueryRow(
		`SELECT count(*) FROM event_outbox
		  WHERE tenant_id=$1 AND envelope->>'type'='relay.drain'
		    AND delivered_at IS NULL`, env.Tenant).Scan(&undelivered)
	if undelivered != 0 {
		t.Fatalf("relay left %d undelivered rows", undelivered)
	}
}

// TestRelay_BadEnvelope_BumpsAttempts: a row whose body is malformed
// JSON shouldn't halt the relay; it should bump attempts + last_error
// and move on. Documented as Skip — Postgres rejects malformed jsonb
// at write time so the case can't actually happen via WriteOutbox,
// but the code path exists so future schema changes (text column?)
// might re-open it.
func TestRelay_BadEnvelope_BumpsAttempts(t *testing.T) {
	t.Skip("malformed jsonb is rejected by Postgres at write time")
}
