package storage_test

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts/storage"
)

// TestDevStub_PutGet: writing then reading the same key returns
// the same bytes and a SHA-256 derived from the body.
func TestDevStub_PutGet(t *testing.T) {
	s := storage.NewDevStub()
	body := []byte("hello evidence")

	obj, err := s.Put(context.Background(), "evidence", "k1", "image/jpeg",
		bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if obj.SHA256 == "" {
		t.Fatal("SHA-256 empty")
	}
	if obj.Size != int64(len(body)) {
		t.Fatalf("size: %d", obj.Size)
	}

	r, got, err := s.Get(context.Background(), "evidence", "k1")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	read, _ := io.ReadAll(r)
	if !bytes.Equal(read, body) {
		t.Fatalf("body round-trip: %q vs %q", read, body)
	}
	if got.SHA256 != obj.SHA256 {
		t.Fatalf("sha mismatch: %s vs %s", got.SHA256, obj.SHA256)
	}
}

// TestDevStub_GetUnknown: a key not previously Put returns an error.
// The reaper relies on this to distinguish "not found" from real
// failures.
func TestDevStub_GetUnknown(t *testing.T) {
	s := storage.NewDevStub()
	_, _, err := s.Get(context.Background(), "evidence", "nope")
	if err == nil {
		t.Fatal("Get on unknown key should error")
	}
}

// TestDevStub_Delete: deleting a key makes subsequent Get fail.
// Deleting an unknown key is a silent no-op (idempotent — the
// reaper depends on this).
func TestDevStub_Delete(t *testing.T) {
	s := storage.NewDevStub()
	_, _ = s.Put(context.Background(), "evidence", "k", "text/plain",
		bytes.NewReader([]byte("x")))
	if err := s.Delete(context.Background(), "evidence", "k"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get(context.Background(), "evidence", "k"); err == nil {
		t.Fatal("Get after Delete should error")
	}
	// Idempotent re-delete.
	if err := s.Delete(context.Background(), "evidence", "k"); err != nil {
		t.Fatalf("re-delete should be a no-op, got: %v", err)
	}
}

// TestDevStub_PresignPut: PresignPut returns a memory:// URL with
// expiry and Content-Type headers. Real adapters return real S3-style
// URLs; this test pins the shape callers depend on.
func TestDevStub_PresignPut(t *testing.T) {
	s := storage.NewDevStub()
	p, err := s.PresignPut(context.Background(), "evidence", "k", "image/jpeg",
		15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if p.Method != "PUT" {
		t.Errorf("method: %s", p.Method)
	}
	if p.Headers["Content-Type"] != "image/jpeg" {
		t.Errorf("content-type header: %s", p.Headers["Content-Type"])
	}
	if p.Expires.Before(time.Now().Add(time.Minute)) {
		t.Errorf("expires too soon: %v", p.Expires)
	}
}
