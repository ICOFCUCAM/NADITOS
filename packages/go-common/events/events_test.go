package events_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/events"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestInProc_Subscribe_FansOut: every subscriber to a given type
// receives every published envelope. Order of dispatch doesn't matter
// here — what matters is that nothing is dropped.
func TestInProc_Subscribe_FansOut(t *testing.T) {
	bus := events.NewInProc(discardLogger())
	var a, b int64
	_ = bus.Subscribe("x", func(_ context.Context, _ events.Envelope) error {
		atomic.AddInt64(&a, 1); return nil
	})
	_ = bus.Subscribe("x", func(_ context.Context, _ events.Envelope) error {
		atomic.AddInt64(&b, 1); return nil
	})
	for i := 0; i < 5; i++ {
		_ = bus.Publish(context.Background(), events.Envelope{Type: "x"})
	}
	if a != 5 || b != 5 {
		t.Fatalf("fan-out: a=%d b=%d", a, b)
	}
}

// TestInProc_TypeFilter: a subscriber to type X only receives X
// envelopes. A different-type publish does NOT fire the handler.
func TestInProc_TypeFilter(t *testing.T) {
	bus := events.NewInProc(discardLogger())
	var got int64
	_ = bus.Subscribe("only-x", func(_ context.Context, _ events.Envelope) error {
		atomic.AddInt64(&got, 1); return nil
	})
	_ = bus.Publish(context.Background(), events.Envelope{Type: "y"})
	_ = bus.Publish(context.Background(), events.Envelope{Type: "z"})
	_ = bus.Publish(context.Background(), events.Envelope{Type: "only-x"})
	if got != 1 {
		t.Fatalf("type filter: got %d, want 1", got)
	}
}

// TestInProc_HandlerErrorDoesNotBlock: a returning handler error is
// logged but must not stop sibling handlers from firing. Producers
// can't afford a single buggy consumer to brick the bus.
func TestInProc_HandlerErrorDoesNotBlock(t *testing.T) {
	bus := events.NewInProc(discardLogger())
	var ranAfter int64
	_ = bus.Subscribe("x", func(_ context.Context, _ events.Envelope) error {
		return errors.New("boom")
	})
	_ = bus.Subscribe("x", func(_ context.Context, _ events.Envelope) error {
		atomic.AddInt64(&ranAfter, 1); return nil
	})
	_ = bus.Publish(context.Background(), events.Envelope{Type: "x"})
	if ranAfter != 1 {
		t.Fatalf("error in earlier handler blocked later one: ranAfter=%d", ranAfter)
	}
}

// TestInProc_NoSubscriberIsSilentNoop: publishing to a type nobody
// listens for must not crash or error.
func TestInProc_NoSubscriberIsSilentNoop(t *testing.T) {
	bus := events.NewInProc(discardLogger())
	if err := bus.Publish(context.Background(), events.Envelope{Type: "ghost"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

// TestInProc_ConcurrentPublishSubscribe: lots of goroutines
// subscribing AND publishing must not race. The InProc bus uses
// RWMutex; this test is the gate that the lock is held correctly.
func TestInProc_ConcurrentPublishSubscribe(t *testing.T) {
	bus := events.NewInProc(discardLogger())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = bus.Subscribe("c", func(_ context.Context, _ events.Envelope) error {
				return nil
			})
		}()
		go func() {
			defer wg.Done()
			_ = bus.Publish(context.Background(), events.Envelope{Type: "c"})
		}()
	}
	wg.Wait()
}

// TestNewEnvelope_FillsRequiredFields: NewEnvelope must produce a
// envelope with non-zero ID, Type, Version, Source, TenantID,
// OccurredAt. Producers depend on these being populated for
// downstream chain-of-custody.
func TestNewEnvelope_FillsRequiredFields(t *testing.T) {
	env := events.NewEnvelope("svc", "t1", "test.event", 1, map[string]string{"k": "v"})
	if env.ID == "" {
		t.Error("ID empty")
	}
	if env.Type != "test.event" {
		t.Errorf("type: %s", env.Type)
	}
	if env.Version != 1 {
		t.Errorf("version: %d", env.Version)
	}
	if env.Source != "svc" {
		t.Errorf("source: %s", env.Source)
	}
	if env.TenantID != "t1" {
		t.Errorf("tenant: %s", env.TenantID)
	}
	if env.OccurredAt.IsZero() {
		t.Error("OccurredAt zero")
	}
	if env.Data == nil {
		t.Error("Data nil")
	}
}
