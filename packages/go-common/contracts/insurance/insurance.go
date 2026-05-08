// Package insurance defines the contract for verifying vehicle insurance
// against the national bureau / EU Green Card system / private aggregators.
package insurance

import (
	"context"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type Policy struct {
	Provider     string
	PolicyNumber string
	StartsAt     time.Time
	ExpiresAt    time.Time
	IsActive     bool
}

type Verifier interface {
	Info() contracts.AdapterInfo
	// VerifyByPlate returns the most recent active policy known to the
	// upstream provider. Returns (nil, nil) if no record exists.
	VerifyByPlate(ctx context.Context, tenantID, plate string) (*Policy, error)
	VerifyByVIN(ctx context.Context, tenantID, vin string) (*Policy, error)
}
