package events

import (
	"context"
	"log/slog"
	"sync"
)

// InProc is the default Publisher used in dev / single-binary deployments.
// It is also a Subscriber so a single process can wire its own handlers.
//
// In production swap for the NATS / Kafka publisher (see nats.go); the
// producing services don't change.
type InProc struct {
	mu   sync.RWMutex
	subs map[string][]Handler
	log  *slog.Logger
}

func NewInProc(log *slog.Logger) *InProc {
	return &InProc{subs: map[string][]Handler{}, log: log}
}

func (p *InProc) Subscribe(eventType string, h Handler) error {
	p.mu.Lock()
	p.subs[eventType] = append(p.subs[eventType], h)
	p.mu.Unlock()
	return nil
}

// Publish dispatches synchronously to all subscribers; handler errors are
// logged but never block the producer. Subscribers must be idempotent.
func (p *InProc) Publish(ctx context.Context, env Envelope) error {
	p.mu.RLock()
	hs := append([]Handler(nil), p.subs[env.Type]...)
	p.mu.RUnlock()
	for _, h := range hs {
		if err := h(ctx, env); err != nil && p.log != nil {
			p.log.Warn("event handler failed",
				slog.String("type", env.Type),
				slog.String("id", env.ID),
				slog.String("err", err.Error()),
			)
		}
	}
	return nil
}

func (p *InProc) Close() error { return nil }
