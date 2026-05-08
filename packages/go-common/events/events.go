// Package events defines NADITOS domain events and the Publisher contract
// services use to broadcast them.
//
// Events are small, immutable, JSON-serializable records describing
// something that happened. Services *publish* events; other services
// (or future ones) *subscribe* to them. Every event carries the same
// envelope so consumers, audit, and observability can rely on the shape.
//
// In Phase 1 the default Publisher is in-process — events are
// dispatched synchronously to local subscribers (used by the audit
// service). Production deployments swap in a NATS / Kafka publisher
// without touching producer code: services depend on the Publisher
// interface, not on a transport.
//
// Schema discipline:
//   - event Type is a stable string ("fine.issued", "vehicle.flagged",…)
//   - event Version is bumped on breaking payload changes
//   - payloads are typed via the matching `*Payload` struct
//   - all timestamps are UTC, all IDs are strings
package events

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Envelope struct {
	ID         string    `json:"id"`           // unique event id
	Type       string    `json:"type"`         // e.g. "fine.issued"
	Version    int       `json:"version"`
	Source     string    `json:"source"`       // emitting service name
	TenantID   string    `json:"tenant_id"`
	OccurredAt time.Time `json:"occurred_at"`
	ActorID    string    `json:"actor_id,omitempty"`
	ActorRole  string    `json:"actor_role,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
	Data       any       `json:"data"`
}

// Publisher is what services call to broadcast events.
type Publisher interface {
	Publish(ctx context.Context, env Envelope) error
	Close() error
}

// Subscriber is the local-bus consumer side. The transport-backed
// publishers wire their own subscribers (NATS Subscribe, Kafka consumer).
type Handler func(ctx context.Context, env Envelope) error

type Subscriber interface {
	Subscribe(eventType string, h Handler) error
}

// New helpers to build well-formed envelopes.
func NewEnvelope(source, tenantID, eventType string, version int, data any) Envelope {
	return Envelope{
		ID:         uuid.NewString(),
		Type:       eventType,
		Version:    version,
		Source:     source,
		TenantID:   tenantID,
		OccurredAt: time.Now().UTC(),
		Data:       data,
	}
}
