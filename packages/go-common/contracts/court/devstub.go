package court

import (
	"context"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
	"github.com/google/uuid"
)

type DevStub struct{}

func NewDevStub() *DevStub { return &DevStub{} }

func (DevStub) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "court", Provider: "dev-stub"}
}

func (DevStub) File(_ context.Context, _ FilePacket) (*CaseRef, error) {
	return &CaseRef{
		ID:      "case_" + uuid.NewString(),
		Court:   "Demo Magistrate",
		FiledAt: time.Now().UTC(),
		Status:  "filed",
	}, nil
}

func (DevStub) Status(_ context.Context, caseID string) (*CaseRef, error) {
	return &CaseRef{ID: caseID, Court: "Demo Magistrate", FiledAt: time.Now().UTC(), Status: "filed"}, nil
}
