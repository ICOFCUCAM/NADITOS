package inspection_test

import (
	"context"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/contracts/inspection"
)

// TestDevStub_VerifyByPlate_NoRecord: the dev stub returns nil, nil
// for any plate — meaning "provider answered, no record on file".
// The worker's persist branch keys off this exact (nil, nil) signal
// to skip the row update without erroring.
func TestDevStub_VerifyByPlate_NoRecord(t *testing.T) {
	d := inspection.NewDevStub()
	rec, err := d.VerifyByPlate(context.Background(), "t1", "AB-12-CD")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rec != nil {
		t.Fatalf("dev stub should return nil record, got %+v", rec)
	}
}

// TestDevStub_VerifyByVIN_NoRecord: same shape via the VIN path.
func TestDevStub_VerifyByVIN_NoRecord(t *testing.T) {
	d := inspection.NewDevStub()
	rec, err := d.VerifyByVIN(context.Background(), "t1", "VIN12345")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rec != nil {
		t.Fatalf("got %+v", rec)
	}
}

// TestDevStub_Info: identity strings are stable.
func TestDevStub_Info(t *testing.T) {
	info := inspection.NewDevStub().Info()
	if info.Module != "inspection" || info.Provider != "dev-stub" {
		t.Fatalf("info: %+v", info)
	}
}
