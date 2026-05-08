package anpr

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

// DevStub returns no reads; the police PWA falls back to manual-entry mode.
// Replace with a real adapter (OpenALPR / PlateRecognizer / custom) in prod.
type DevStub struct{}

func NewDevStub() *DevStub { return &DevStub{} }

func (DevStub) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "anpr", Provider: "dev-stub"}
}

func (DevStub) Recognize(_ context.Context, _ Image, _ RecognizeOpts) ([]Read, error) {
	return nil, nil
}
