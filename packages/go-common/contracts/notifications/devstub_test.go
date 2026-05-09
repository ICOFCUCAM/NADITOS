package notifications_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/contracts/notifications"
)

// TestDevStub_Send_ReturnsReceipt: Send always succeeds and returns a
// receipt with status="queued" and a non-empty id. The notifications
// consumer relies on this to record delivery in notification_records.
func TestDevStub_Send_ReturnsReceipt(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := notifications.NewDevStub(log)
	r, err := d.Send(context.Background(), notifications.Message{
		Channel: "email", To: "x@y", Subject: "hi", Body: "hello", TenantID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.Status != "queued" || r.Provider != "dev-stub" {
		t.Fatalf("receipt: %+v", r)
	}
}

// TestDevStub_Send_LogsContents: when a logger is supplied, Send
// emits a structured log line carrying channel/to/subject/tenant.
// Operators tail this in dev to see what would have shipped.
func TestDevStub_Send_LogsContents(t *testing.T) {
	buf := &strings.Builder{}
	log := slog.New(slog.NewTextHandler(textBuf{buf}, nil))
	d := notifications.NewDevStub(log)
	_, _ = d.Send(context.Background(), notifications.Message{
		Channel: "sms", To: "+1-555", Subject: "subj", Body: "body", TenantID: "t1",
	})
	got := buf.String()
	if !strings.Contains(got, "channel=sms") {
		t.Errorf("missing channel: %s", got)
	}
	if !strings.Contains(got, "to=+1-555") {
		t.Errorf("missing to: %s", got)
	}
	if !strings.Contains(got, "tenant=t1") {
		t.Errorf("missing tenant: %s", got)
	}
}

// TestDevStub_Send_NilLogger: a stub built without a logger must not
// crash on Send. Test fixtures pass nil routinely.
func TestDevStub_Send_NilLogger(t *testing.T) {
	d := notifications.NewDevStub(nil)
	if _, err := d.Send(context.Background(), notifications.Message{
		Channel: "email", To: "x", Subject: "y", Body: "z", TenantID: "t",
	}); err != nil {
		t.Fatal(err)
	}
}

type textBuf struct{ b *strings.Builder }

func (w textBuf) Write(p []byte) (int, error) { return w.b.Write(p) }
