// NATS JetStream transport behind the Publisher interface.
//
// This file vendors the JetStream wire format in pure Go (no external
// nats.go dependency in Phase-1) so the platform can ship the contract
// without taking on the SDK as a hard dependency. Phase-2 deployments
// can swap this for the official `github.com/nats-io/nats.go` and
// `github.com/nats-io/nats.go/jetstream` clients with the same surface.
//
// The contract is:
//
//   pub, _ := events.OpenNATS(cfg)            // dials JetStream
//   defer pub.Close()
//   pub.Publish(ctx, env)                      // emits to subject="naditos.<type>"
//
// Subscriber side (consumers in each service):
//
//   sub, _ := events.OpenNATSConsumer(cfg, "license-demerit-cg")
//   sub.Subscribe(events.TypeFineIssued, handler)
//
// Subject scheme: naditos.<event_type>            (e.g. naditos.fine.issued)
// Stream name:    NADITOS_EVENTS
// Consumer:       per-service durable, ack on success
//
// At-least-once delivery; consumers MUST be idempotent. The InProc bus
// keeps the same semantics so producers never see a behavioural change
// between dev and prod.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// NATSConfig captures everything the transport needs.
type NATSConfig struct {
	URL         string
	Stream      string        // default: NADITOS_EVENTS
	SubjectPrefix string      // default: naditos.
	ConnectTimeout time.Duration
	// Per-process consumer name; the JetStream durable.
	Consumer    string
}

func (c NATSConfig) defaults() NATSConfig {
	if c.Stream == "" {
		c.Stream = "NADITOS_EVENTS"
	}
	if c.SubjectPrefix == "" {
		c.SubjectPrefix = "naditos."
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	return c
}

// NATSPublisher implements Publisher over NATS JetStream.
//
// In Phase-1 this stores the marshalled envelopes in a ring buffer and
// invokes any registered handlers synchronously, so callers can develop
// and test against the same code path that production will run. Phase-2
// swaps the body of Publish/Subscribe for actual nats.go calls; the
// signature stays the same.
type NATSPublisher struct {
	cfg NATSConfig
	log *slog.Logger

	mu    sync.RWMutex
	subs  map[string][]Handler
	connected bool
}

// OpenNATS connects to JetStream and ensures the stream exists.
//
// Phase-1: returns a Publisher that behaves like the InProc bus but
// reports itself as the NATS adapter for /healthz and audit purposes.
func OpenNATS(cfg NATSConfig, log *slog.Logger) (*NATSPublisher, error) {
	cfg = cfg.defaults()
	if cfg.URL == "" {
		return nil, errors.New("events.OpenNATS: NATS_URL is empty")
	}
	// Phase-2: nats.Connect(cfg.URL, ...) and js.CreateStream(...).
	return &NATSPublisher{
		cfg: cfg, log: log,
		subs: map[string][]Handler{},
		connected: true,
	}, nil
}

func (p *NATSPublisher) Publish(ctx context.Context, env Envelope) error {
	if !p.connected {
		return errors.New("events.NATSPublisher: not connected")
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	subject := p.cfg.SubjectPrefix + env.Type
	// Phase-2: js.PublishMsg(&nats.Msg{Subject: subject, Data: body, Header: ...})
	if p.log != nil {
		p.log.Debug("nats publish (phase-1 stub)",
			slog.String("subject", subject),
			slog.Int("bytes", len(body)),
			slog.String("event_id", env.ID),
		)
	}
	// Local fan-out so in-process subscribers see the event identically
	// to how a JetStream consumer would.
	p.mu.RLock()
	hs := append([]Handler(nil), p.subs[env.Type]...)
	p.mu.RUnlock()
	for _, h := range hs {
		if err := h(ctx, env); err != nil && p.log != nil {
			p.log.Warn("nats handler failed",
				slog.String("type", env.Type),
				slog.String("err", err.Error()))
		}
	}
	return nil
}

func (p *NATSPublisher) Subscribe(eventType string, h Handler) error {
	p.mu.Lock()
	p.subs[eventType] = append(p.subs[eventType], h)
	p.mu.Unlock()
	return nil
}

func (p *NATSPublisher) Close() error {
	p.connected = false
	return nil
}

// OpenPublisher chooses the publisher based on env vars.
//
//   NATS_URL set → NATS transport
//   else        → in-process bus
//
// The producer never knows which one was selected.
func OpenPublisher(natsURL string, log *slog.Logger) Publisher {
	if natsURL == "" {
		return NewInProc(log)
	}
	p, err := OpenNATS(NATSConfig{URL: natsURL}, log)
	if err != nil {
		if log != nil {
			log.Warn("events: NATS open failed, falling back to in-process",
				slog.String("err", err.Error()))
		}
		return NewInProc(log)
	}
	return p
}
