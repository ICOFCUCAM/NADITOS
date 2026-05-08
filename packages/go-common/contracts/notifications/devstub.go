package notifications

import (
	"context"
	"log/slog"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
	"github.com/google/uuid"
)

// DevStub logs the message and pretends to deliver it.
type DevStub struct{ Log *slog.Logger }

func NewDevStub(log *slog.Logger) *DevStub { return &DevStub{Log: log} }

func (DevStub) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "notifications", Provider: "dev-stub"}
}

func (d DevStub) Send(_ context.Context, m Message) (*Receipt, error) {
	if d.Log != nil {
		d.Log.Info("notify (stub)",
			slog.String("channel", string(m.Channel)),
			slog.String("to", m.To),
			slog.String("subject", m.Subject),
			slog.String("tenant", m.TenantID),
		)
	}
	return &Receipt{ID: uuid.NewString(), Status: "queued", Provider: "dev-stub"}, nil
}
