package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

// DevStub is an in-memory store for local dev / tests. Not for production.
type DevStub struct {
	mu  sync.Mutex
	obj map[string]*storedObj
}

type storedObj struct {
	o    Object
	body []byte
}

func NewDevStub() *DevStub { return &DevStub{obj: map[string]*storedObj{}} }

func (DevStub) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "storage", Provider: "dev-stub"}
}

func (s *DevStub) PresignPut(_ context.Context, bucket, key, contentType string, ttl time.Duration) (*PresignedPut, error) {
	return &PresignedPut{
		URL:     "memory://" + bucket + "/" + key,
		Method:  "PUT",
		Headers: map[string]string{"Content-Type": contentType},
		Expires: time.Now().Add(ttl).UTC(),
	}, nil
}

func (s *DevStub) Put(_ context.Context, bucket, key, contentType string, body io.Reader) (*Object, error) {
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, body); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(buf.Bytes())
	o := Object{
		Bucket: bucket, Key: key, ContentType: contentType,
		Size: int64(buf.Len()), SHA256: hex.EncodeToString(sum[:]),
		UploadedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	s.obj[bucket+"/"+key] = &storedObj{o: o, body: buf.Bytes()}
	s.mu.Unlock()
	return &o, nil
}

func (s *DevStub) Get(_ context.Context, bucket, key string) (io.ReadCloser, *Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.obj[bucket+"/"+key]
	if !ok {
		return nil, nil, errors.New("storage: not found")
	}
	return io.NopCloser(bytes.NewReader(v.body)), &v.o, nil
}

func (s *DevStub) Delete(_ context.Context, bucket, key string) error {
	s.mu.Lock()
	delete(s.obj, bucket+"/"+key)
	s.mu.Unlock()
	return nil
}
