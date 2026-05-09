package payments_test

import (
	"context"
	"errors"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/contracts/payments"
)

// TestDevStub_CreateIntent_AutoSucceeds: the dev stub auto-succeeds
// every intent. Smoke + dev workflows depend on this.
func TestDevStub_CreateIntent_AutoSucceeds(t *testing.T) {
	d := payments.NewDevStub()
	in, err := d.CreateIntent(context.Background(), payments.CreateIntentInput{
		TenantID: "t1",
		Amount:   payments.Money{Amount: "100.00", Currency: "EUR"},
		Method:   "card",
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.Status != payments.StatusSucceeded {
		t.Fatalf("status: %s", in.Status)
	}
	if in.ID == "" {
		t.Fatal("intent id empty")
	}
	if in.Amount.Amount != "100.00" || in.Amount.Currency != "EUR" {
		t.Fatalf("amount: %+v", in.Amount)
	}
}

// TestDevStub_GetIntent_RoundTrip: a CreateIntent's id is retrievable
// via GetIntent. Tests that downstream code (e.g. webhooks) can
// resolve an intent by id.
func TestDevStub_GetIntent_RoundTrip(t *testing.T) {
	d := payments.NewDevStub()
	in, _ := d.CreateIntent(context.Background(), payments.CreateIntentInput{
		TenantID: "t1",
		Amount:   payments.Money{Amount: "1", Currency: "EUR"},
	})
	got, err := d.GetIntent(context.Background(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != in.ID {
		t.Fatalf("id round-trip: %s vs %s", got.ID, in.ID)
	}
}

// TestDevStub_GetIntent_Unknown: an id that was never created returns
// ErrNotFound (not nil + nil).
func TestDevStub_GetIntent_Unknown(t *testing.T) {
	d := payments.NewDevStub()
	_, err := d.GetIntent(context.Background(), "nope")
	if !errors.Is(err, payments.ErrNotFound) {
		t.Fatalf("err: %v", err)
	}
}

// TestDevStub_Refund_FlipsStatus: Refund on an existing intent flips
// status to Refunded. Refund on an unknown intent returns ErrNotFound.
func TestDevStub_Refund_FlipsStatus(t *testing.T) {
	d := payments.NewDevStub()
	in, _ := d.CreateIntent(context.Background(), payments.CreateIntentInput{
		TenantID: "t1",
		Amount:   payments.Money{Amount: "1", Currency: "EUR"},
	})
	if err := d.Refund(context.Background(), in.ID, nil, "test"); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetIntent(context.Background(), in.ID)
	if got.Status != payments.StatusRefunded {
		t.Fatalf("status after refund: %s", got.Status)
	}
	if err := d.Refund(context.Background(), "nope", nil, ""); !errors.Is(err, payments.ErrNotFound) {
		t.Fatalf("refund unknown: %v", err)
	}
}

// TestDevStub_VerifyWebhook_AlwaysRejects: the dev stub deliberately
// rejects every webhook so tests exercising the failure path don't
// have to fake a signature scheme. Real adapters override this.
func TestDevStub_VerifyWebhook_AlwaysRejects(t *testing.T) {
	d := payments.NewDevStub()
	_, err := d.VerifyWebhook(context.Background(), nil, []byte("anything"))
	if !errors.Is(err, payments.ErrSignatureInvalid) {
		t.Fatalf("err: %v", err)
	}
}

// TestDevStub_Info: provider identity is stable so the health monitor
// + audit dashboards key off "payments" / "dev-stub".
func TestDevStub_Info(t *testing.T) {
	d := payments.NewDevStub()
	info := d.Info()
	if info.Module != "payments" {
		t.Errorf("module: %s", info.Module)
	}
	if info.Provider != "dev-stub" {
		t.Errorf("provider: %s", info.Provider)
	}
}
