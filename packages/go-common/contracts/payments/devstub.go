package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

// DevStub is an in-memory provider that auto-succeeds every intent.
// It's the default adapter wired for local dev and CI; never use in prod.
type DevStub struct {
	mu      sync.Mutex
	intents map[string]*Intent
}

func NewDevStub() *DevStub {
	return &DevStub{intents: map[string]*Intent{}}
}

func (d *DevStub) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "payments", Provider: "dev-stub"}
}

func (d *DevStub) CreateIntent(ctx context.Context, in CreateIntentInput) (*Intent, error) {
	id := "dev_" + randHex(16)
	it := &Intent{
		ID:        id,
		Status:    StatusSucceeded,
		Amount:    in.Amount,
		Metadata:  in.Metadata,
		CreatedAt: time.Now().UTC(),
	}
	d.mu.Lock()
	d.intents[id] = it
	d.mu.Unlock()
	return it, nil
}

func (d *DevStub) GetIntent(ctx context.Context, id string) (*Intent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	it, ok := d.intents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return it, nil
}

func (d *DevStub) Refund(ctx context.Context, intentID string, amount *Money, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	it, ok := d.intents[intentID]
	if !ok {
		return ErrNotFound
	}
	it.Status = StatusRefunded
	return nil
}

func (d *DevStub) VerifyWebhook(_ context.Context, _ map[string]string, _ []byte) (*WebhookEvent, error) {
	return nil, ErrSignatureInvalid
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
