package inspection

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type DevStub struct{}

func NewDevStub() *DevStub { return &DevStub{} }

func (DevStub) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "inspection", Provider: "dev-stub"}
}

func (DevStub) VerifyByPlate(_ context.Context, _, _ string) (*Record, error) { return nil, nil }
func (DevStub) VerifyByVIN(_ context.Context, _, _ string) (*Record, error)   { return nil, nil }
