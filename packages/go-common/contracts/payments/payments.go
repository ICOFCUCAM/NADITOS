// Package payments defines the payment-processor contract used by the
// fines service. Real adapters (Stripe, Adyen, treasury) sit beside the
// dev stub.
package payments

import (
	"context"
	"errors"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type Money struct {
	Amount   string // decimal as string to avoid float drift
	Currency string // ISO-4217
}

type Intent struct {
	ID            string            // provider ref
	Status        Status
	Amount        Money
	ReturnURL     string            // for redirect-based methods
	ClientSecret  string            // for SDK confirmation
	Metadata      map[string]string
	CreatedAt     time.Time
}

type Status string

const (
	StatusRequiresAction  Status = "requires_action"
	StatusProcessing      Status = "processing"
	StatusSucceeded       Status = "succeeded"
	StatusFailed          Status = "failed"
	StatusRefunded        Status = "refunded"
	StatusCancelled       Status = "cancelled"
)

type CreateIntentInput struct {
	TenantID      string
	IdempotencyKey string
	Amount        Money
	Description   string
	Method        string            // "card" | "mobile" | "treasury" | ...
	Customer      *Customer
	Metadata      map[string]string
}

type Customer struct {
	ID    string
	Email string
	Phone string
}

type WebhookEvent struct {
	ID        string
	Type      string            // e.g. "intent.succeeded"
	IntentID  string
	Status    Status
	Raw       []byte            // original provider payload (verified)
	OccurredAt time.Time
}

// Provider is what the fines service depends on; never imports a vendor SDK directly.
type Provider interface {
	Info() contracts.AdapterInfo
	CreateIntent(ctx context.Context, in CreateIntentInput) (*Intent, error)
	GetIntent(ctx context.Context, id string) (*Intent, error)
	Refund(ctx context.Context, intentID string, amount *Money, reason string) error
	// VerifyWebhook validates a provider-signed payload and returns a
	// canonicalized event the caller can act on.
	VerifyWebhook(ctx context.Context, headers map[string]string, body []byte) (*WebhookEvent, error)
}

var ErrNotFound = errors.New("payments: intent not found")
var ErrSignatureInvalid = errors.New("payments: webhook signature invalid")
