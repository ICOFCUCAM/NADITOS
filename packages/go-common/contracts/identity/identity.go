// Package identity defines the contract for sovereign identity providers
// (eIDAS, national civil registry, BankID, MyInfo, etc.). These adapters
// confirm that a citizen with a national ID is who they claim to be and
// optionally return verified profile attributes.
package identity

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type Subject struct {
	NationalID  string
	FullName    string
	DateOfBirth string // YYYY-MM-DD
	Address     string
	Verified    bool
	Provider    string
}

type AssertionRequest struct {
	TenantID  string
	ReturnURL string
	Nonce     string
}

type Assertion struct {
	URL string // redirect the user here
	ID  string // server-side handle to redeem
}

type Provider interface {
	Info() contracts.AdapterInfo
	Begin(ctx context.Context, req AssertionRequest) (*Assertion, error)
	Resolve(ctx context.Context, id string) (*Subject, error)
}
