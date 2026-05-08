package insurance

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type DevStub struct{}

func NewDevStub() *DevStub { return &DevStub{} }

func (DevStub) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "insurance", Provider: "dev-stub"}
}

func (DevStub) VerifyByPlate(_ context.Context, _, _ string) (*Policy, error) { return nil, nil }
func (DevStub) VerifyByVIN(_ context.Context, _, _ string) (*Policy, error)   { return nil, nil }
