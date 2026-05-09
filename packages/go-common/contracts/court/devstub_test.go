package court_test

import (
	"context"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/contracts/court"
)

// TestDevStub_File_ReturnsCaseRef: filing a packet always succeeds in
// the dev stub and returns a case_ ref with status="filed" — what the
// fines-escalation engine expects when it pushes a stage-5 case to
// the magistrate.
func TestDevStub_File_ReturnsCaseRef(t *testing.T) {
	d := court.NewDevStub()
	ref, err := d.File(context.Background(), court.FilePacket{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref.ID, "case_") {
		t.Errorf("id prefix: %s", ref.ID)
	}
	if ref.Status != "filed" {
		t.Errorf("status: %s", ref.Status)
	}
	if ref.Court == "" {
		t.Error("court empty")
	}
	if ref.FiledAt.IsZero() {
		t.Error("filed_at zero")
	}
}

// TestDevStub_Status_RoundTrip: Status with a known id returns a
// CaseRef carrying that id back. Lets callers (e.g. UI status pages)
// confirm a case is on file without parsing the original packet.
func TestDevStub_Status_RoundTrip(t *testing.T) {
	d := court.NewDevStub()
	ref, err := d.Status(context.Background(), "case_abc")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "case_abc" {
		t.Errorf("id: %s", ref.ID)
	}
}

// TestDevStub_Info: identity is stable for the health monitor and
// the admin /providers tile.
func TestDevStub_Info(t *testing.T) {
	info := court.NewDevStub().Info()
	if info.Module != "court" {
		t.Errorf("module: %s", info.Module)
	}
	if info.Provider != "dev-stub" {
		t.Errorf("provider: %s", info.Provider)
	}
}
