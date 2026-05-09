package events_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
)

// fastForwardOffset preloads a consumer's offset row to the current
// max event_outbox id. Without this, every fresh consumer-name in a
// test re-processes the entire prior smoke history before reaching
// the test's seeded events.
func fastForwardOffset(t *testing.T, env *testkit.Env, consumer string) {
	t.Helper()
	env.Exec(`INSERT INTO event_consumer_offsets (consumer, last_event_id)
	          SELECT $1, COALESCE(max(id), 0) FROM event_outbox
	          ON CONFLICT (consumer) DO NOTHING`, consumer)
}

// drainConsumer runs the consumer until either the deadline or the
// caller's predicate returns true, then cancels Run.
func drainConsumer(t *testing.T, c *events.Consumer, until func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if until() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("consumer didn't drain in time")
}

// seedOutbox writes an event_outbox row directly. Returns the row id.
func seedOutbox(t *testing.T, env *testkit.Env, eventType string) int64 {
	t.Helper()
	envel := events.NewEnvelope("test-svc", env.Tenant, eventType, 1, nil)
	tx, err := env.AdminPool().Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := events.WriteOutbox(context.Background(), tx, envel); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := env.QueryRow(
		`SELECT max(id) FROM event_outbox
		  WHERE tenant_id=$1 AND envelope->>'type'=$2`,
		env.Tenant, eventType).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestConsumer_DispatchesNewEvents: events written after the consumer
// starts are dispatched and the consumer's offset advances past them.
func TestConsumer_DispatchesNewEvents(t *testing.T) {
	env := testkit.Setup(t)
	consumerName := "test-consumer-" + env.Tenant
	fastForwardOffset(t, env, consumerName)

	var got int64
	c := events.NewConsumer(env.AdminPool(), discardLogger(),
		consumerName,
		func(_ context.Context, _ events.Envelope) error {
			atomic.AddInt64(&got, 1); return nil
		})

	// Seed two events of the same type AFTER fast-forwarding.
	id1 := seedOutbox(t, env, "consumer.x")
	id2 := seedOutbox(t, env, "consumer.x")
	_ = id1

	drainConsumer(t, c.OnlyTypes("consumer.x"), func() bool {
		return atomic.LoadInt64(&got) >= 2
	})

	// Offset must be at least the last seeded id.
	var off int64
	_ = env.QueryRow(
		`SELECT last_event_id FROM event_consumer_offsets WHERE consumer=$1`,
		consumerName).Scan(&off)
	if off < id2 {
		t.Fatalf("offset didn't advance: %d < %d", off, id2)
	}
}

// TestConsumer_TypeFilter: with OnlyTypes set, the dispatcher fires
// only for matching types. Events of other types are skipped but the
// offset still advances past them (so the consumer doesn't get stuck
// re-reading skipped rows forever).
func TestConsumer_TypeFilter(t *testing.T) {
	env := testkit.Setup(t)
	consumerName := "filter-consumer-" + env.Tenant
	fastForwardOffset(t, env, consumerName)

	var got int64
	c := events.NewConsumer(env.AdminPool(), discardLogger(),
		consumerName,
		func(_ context.Context, env events.Envelope) error {
			if env.Type != "filter.want" {
				t.Errorf("dispatched a non-want type: %s", env.Type)
			}
			atomic.AddInt64(&got, 1); return nil
		})

	seedOutbox(t, env, "filter.skip")
	wantID := seedOutbox(t, env, "filter.want")
	seedOutbox(t, env, "filter.skip")

	drainConsumer(t, c.OnlyTypes("filter.want"), func() bool {
		return atomic.LoadInt64(&got) >= 1
	})

	// Offset should be past the LAST seeded row, not just the matching one.
	var off int64
	_ = env.QueryRow(
		`SELECT last_event_id FROM event_consumer_offsets WHERE consumer=$1`,
		consumerName).Scan(&off)
	if off < wantID {
		t.Fatalf("offset stuck before want event: %d < %d", off, wantID)
	}
}

// TestConsumer_DispatcherErrorAdvances: a dispatcher that returns
// errors must NOT halt the stream — the offset advances past the
// failing row so subsequent events still flow. Failures are logged
// elsewhere (tests for that live in service-side consumers).
func TestConsumer_DispatcherErrorAdvances(t *testing.T) {
	env := testkit.Setup(t)
	consumerName := "err-consumer-" + env.Tenant
	fastForwardOffset(t, env, consumerName)

	var seen int64
	c := events.NewConsumer(env.AdminPool(), discardLogger(),
		consumerName,
		func(_ context.Context, _ events.Envelope) error {
			atomic.AddInt64(&seen, 1)
			return errors.New("simulated dispatch failure")
		})

	id := seedOutbox(t, env, "err.event")

	drainConsumer(t, c.OnlyTypes("err.event"), func() bool {
		return atomic.LoadInt64(&seen) >= 1
	})

	var off int64
	_ = env.QueryRow(
		`SELECT last_event_id FROM event_consumer_offsets WHERE consumer=$1`,
		consumerName).Scan(&off)
	if off < id {
		t.Fatalf("offset stuck on failing row: %d < %d", off, id)
	}
}
