package insurance_test

import (
	"context"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/contracts/insurance"
)

// TestDevStub_VerifyByPlate_NoPolicy: the stub answers (nil, nil) for
// every plate. The worker keys off this signal to skip persist.
func TestDevStub_VerifyByPlate_NoPolicy(t *testing.T) {
	d := insurance.NewDevStub()
	pol, err := d.VerifyByPlate(context.Background(), "t1", "AB-12-CD")
	if err != nil {
		t.Fatal(err)
	}
	if pol != nil {
		t.Fatalf("got %+v", pol)
	}
}

func TestDevStub_VerifyByVIN_NoPolicy(t *testing.T) {
	d := insurance.NewDevStub()
	pol, err := d.VerifyByVIN(context.Background(), "t1", "VIN12345")
	if err != nil {
		t.Fatal(err)
	}
	if pol != nil {
		t.Fatalf("got %+v", pol)
	}
}

// TestDevStub_Info: identity for the health monitor + admin tile.
func TestDevStub_Info(t *testing.T) {
	info := insurance.NewDevStub().Info()
	if info.Module != "insurance" || info.Provider != "dev-stub" {
		t.Fatalf("info: %+v", info)
	}
}
