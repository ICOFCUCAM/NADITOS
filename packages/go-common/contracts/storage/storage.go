// Package storage defines the contract for evidence/object storage.
// Real adapters: S3, GCS, Azure Blob, MinIO, sovereign clouds.
package storage

import (
	"context"
	"io"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type Object struct {
	Bucket      string
	Key         string
	ContentType string
	Size        int64
	SHA256      string
	UploadedAt  time.Time
}

type PresignedPut struct {
	URL    string
	Method string
	Headers map[string]string
	Expires time.Time
}

type Store interface {
	Info() contracts.AdapterInfo
	// PresignPut returns a short-lived URL the client uploads to directly.
	// The server records the resulting object key; eventual SHA-256 is
	// computed by the client and verified server-side at retrieval.
	PresignPut(ctx context.Context, bucket, key string, contentType string, ttl time.Duration) (*PresignedPut, error)
	Put(ctx context.Context, bucket, key, contentType string, body io.Reader) (*Object, error)
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, *Object, error)
	Delete(ctx context.Context, bucket, key string) error
}
