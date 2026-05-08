// Package inspection defines the contract for verifying vehicle
// roadworthiness against a national inspection station network or its
// equivalent (EU PTI / TÜV / ITV / contrôle technique).
package inspection

import (
	"context"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type Record struct {
	Station       string
	PerformedAt   time.Time
	ExpiresAt     time.Time
	Result        string // pass|fail|conditional
	CertificateURL string
}

type Verifier interface {
	Info() contracts.AdapterInfo
	VerifyByPlate(ctx context.Context, tenantID, plate string) (*Record, error)
	VerifyByVIN(ctx context.Context, tenantID, vin string) (*Record, error)
}
