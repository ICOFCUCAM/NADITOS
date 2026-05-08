// Package court defines the contract for escalating fines into the
// judicial workflow once administrative remedies are exhausted.
package court

import (
	"context"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type CaseRef struct {
	ID         string    // upstream case id
	Court      string
	FiledAt    time.Time
	Status     string    // filed | scheduled | judged | dismissed
}

type FilePacket struct {
	TenantID    string
	FineID      string
	OffenceCode string
	Plate       string
	OwnerName   string
	OwnerAddress string
	EvidenceURLs []string
	Amount      string
	Currency    string
	IssuedAt    time.Time
}

type Provider interface {
	Info() contracts.AdapterInfo
	File(ctx context.Context, p FilePacket) (*CaseRef, error)
	Status(ctx context.Context, caseID string) (*CaseRef, error)
}
