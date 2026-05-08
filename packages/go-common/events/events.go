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
	TraceID    string    `json:"trace_id,omitempty"`     // W3C trace id for correlation
	RequestID  string    `json:"request_id,omitempty"`   // X-Request-Id of originating request
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

// EnvelopeFromContext is NewEnvelope plus actor + trace propagation
// pulled from the request context (set by observability.Middleware and
// auth middleware). Use this in handlers; use NewEnvelope in workers
// where there's no request context.
func EnvelopeFromContext(ctx ContextLike, source, tenantID, eventType string, version int, data any) Envelope {
	env := NewEnvelope(source, tenantID, eventType, version, data)
	if c, ok := actorFrom(ctx); ok {
		env.ActorID = c.subject
		env.ActorRole = c.role
	}
	if id, ok := traceFrom(ctx); ok {
		env.TraceID = id
	}
	return env
}

// ContextLike avoids importing context here from callers that prefer to
// pass plain context.Context; both shapes work.
type ContextLike interface {
	Value(any) any
}

// actorFrom / traceFrom decouple the events package from the auth and
// observability packages — those packages register the value extractors
// at init() to avoid an import cycle.
type actorClaims struct{ subject, role string }

var (
	actorExtractor func(ContextLike) (actorClaims, bool) = func(ContextLike) (actorClaims, bool) { return actorClaims{}, false }
	traceExtractor func(ContextLike) (string, bool)      = func(ContextLike) (string, bool) { return "", false }
)

func actorFrom(ctx ContextLike) (actorClaims, bool) { return actorExtractor(ctx) }
func traceFrom(ctx ContextLike) (string, bool)      { return traceExtractor(ctx) }

// RegisterActorExtractor lets the auth package wire JWT claims lookup
// without importing this package — call once at process init().
func RegisterActorExtractor(fn func(ContextLike) (subject, role string, ok bool)) {
	actorExtractor = func(ctx ContextLike) (actorClaims, bool) {
		s, r, ok := fn(ctx)
		return actorClaims{subject: s, role: r}, ok
	}
}

// RegisterTraceExtractor lets the observability package wire trace-id
// propagation without an import cycle.
func RegisterTraceExtractor(fn func(ContextLike) (string, bool)) {
	traceExtractor = fn
}
