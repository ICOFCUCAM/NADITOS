package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts"
	"github.com/icofcucam/naditos/packages/go-common/contracts/anpr"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/anpr-gateway/internal/api"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// stubRecognizer captures incoming images and returns whatever reads /
// error the test asks for.
type stubRecognizer struct {
	mu      sync.Mutex
	gotBytes []byte
	gotCT   string
	gotOpts anpr.RecognizeOpts
	reads   []anpr.Read
	err     error
}

func (s *stubRecognizer) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "anpr", Provider: "stub"}
}
func (s *stubRecognizer) Recognize(_ context.Context, img anpr.Image, opts anpr.RecognizeOpts) ([]anpr.Read, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gotBytes = append([]byte(nil), img.Bytes...)
	s.gotCT = img.ContentType
	s.gotOpts = opts
	if s.err != nil {
		return nil, s.err
	}
	return s.reads, nil
}

func build(env *testkit.Env, rec anpr.Recognizer) http.Handler {
	hm := connectors.NewHealthMonitor(env.AdminPool())
	return api.New(env.Cfg, discardLogger(), env.Pool, env.Issuer, rec, hm)
}

// imageRequest builds a multipart/form-data POST against /v1/anpr/recognize.
func imageRequest(t *testing.T, env *testkit.Env, tok string, image []byte, opts map[string]string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for k, v := range opts {
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile("image", "frame.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(image); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/anpr/recognize", body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-Tenant-Id", env.Tenant)
	r.Header.Set("Authorization", "Bearer "+tok)
	return r
}

// TestRecognize_HappyPath: officer uploads bytes; the stub recognizer's
// reads come back as ranked candidates in the JSON response.
func TestRecognize_HappyPath(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{
		reads: []anpr.Read{
			{Plate: "ABC123", Confidence: 0.94, Region: "us-ca"},
			{Plate: "ABCI23", Confidence: 0.72, Region: "us-ca"},
		},
	}
	tok, _ := env.Token("officer", "anpr:scan")
	h := build(env, rec)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, imageRequest(t, env, tok, []byte("fake-jpg-bytes"), map[string]string{
		"country":  "us",
		"min_conf": "0.5",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}

	// Recognizer received our bytes + opts.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if string(rec.gotBytes) != "fake-jpg-bytes" {
		t.Fatalf("recognizer got %q", string(rec.gotBytes))
	}
	if rec.gotOpts.Country != "us" || rec.gotOpts.TenantID != env.Tenant {
		t.Fatalf("opts: %+v", rec.gotOpts)
	}
	if rec.gotOpts.MinConf < 0.49 || rec.gotOpts.MinConf > 0.51 {
		t.Fatalf("min_conf not parsed: %f", rec.gotOpts.MinConf)
	}

	var out struct {
		Provider string `json:"provider"`
		Reads    []struct {
			Plate      string  `json:"plate"`
			Confidence float32 `json:"confidence"`
		} `json:"reads"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Provider != "stub" {
		t.Fatalf("provider: %s", out.Provider)
	}
	if len(out.Reads) != 2 || out.Reads[0].Plate != "ABC123" {
		t.Fatalf("reads: %+v", out.Reads)
	}
}

// TestRecognize_MissingImage rejects requests without an image part.
func TestRecognize_MissingImage(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{}
	tok, _ := env.Token("officer", "anpr:scan")

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("country", "us")
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/v1/anpr/recognize", body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-Tenant-Id", env.Tenant)
	r.Header.Set("Authorization", "Bearer "+tok)

	w := httptest.NewRecorder()
	build(env, rec).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing_image") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// TestRecognize_EmptyImage rejects an upload with zero bytes.
func TestRecognize_EmptyImage(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{}
	tok, _ := env.Token("officer", "anpr:scan")

	w := httptest.NewRecorder()
	build(env, rec).ServeHTTP(w, imageRequest(t, env, tok, []byte{}, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "empty_image") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// TestRecognize_RBAC_Forbids non-officers from hitting the endpoint.
func TestRecognize_RBAC(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{}
	tok, _ := env.Token("citizen") // no anpr:scan
	w := httptest.NewRecorder()
	build(env, rec).ServeHTTP(w, imageRequest(t, env, tok, []byte("x"), nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
}

// TestRecognize_UpstreamError_Maps502: provider failures return 502 so
// the gateway's per-route rate-limit / health-monitor picks them up.
func TestRecognize_UpstreamError_Maps502(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{err: errors.New("upstream unavailable")}
	tok, _ := env.Token("officer", "anpr:scan")
	w := httptest.NewRecorder()
	build(env, rec).ServeHTTP(w, imageRequest(t, env, tok, []byte("x"), nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "upstream unavailable") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// TestHealth: the /v1/anpr/health endpoint reports which provider is
// currently bound — useful for ops dashboards.
func TestHealth(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{}
	tok, _ := env.Token("officer", "anpr:scan")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/anpr/health", nil)
	r.Header.Set("X-Tenant-Id", env.Tenant)
	r.Header.Set("Authorization", "Bearer "+tok)
	build(env, rec).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"provider":"stub"`) {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// TestHealth_StreakBumpedOnFailure: provider errors increment the
// fail_streak via connectors.HealthMonitor; subsequent /health calls
// reflect the streak so the admin /providers tile shows degraded /
// down state without per-module bespoke wiring.
func TestHealth_StreakBumpedOnFailure(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{err: errors.New("upstream down")}
	tok, _ := env.Token("officer", "anpr:scan")
	h := build(env, rec)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, imageRequest(t, env, tok, []byte("x"), nil))
		if w.Code != http.StatusBadGateway {
			t.Fatalf("attempt %d: want 502, got %d", i, w.Code)
		}
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/anpr/health", nil)
	r.Header.Set("X-Tenant-Id", env.Tenant)
	r.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("health: %d %s", w.Code, body)
	}
	if !strings.Contains(body, `"state":"degraded"`) {
		t.Fatalf("want state=degraded, got %s", body)
	}
	if !strings.Contains(body, `"fail_streak":3`) {
		t.Fatalf("want streak=3, got %s", body)
	}
}

// TestHealth_StreakResetsOnSuccess: a single successful recognize
// after failures clears the streak and flips state back to ok.
func TestHealth_StreakResetsOnSuccess(t *testing.T) {
	env := testkit.Setup(t)
	rec := &stubRecognizer{err: errors.New("flaky upstream")}
	tok, _ := env.Token("officer", "anpr:scan")
	h := build(env, rec)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, imageRequest(t, env, tok, []byte("x"), nil))
		if w.Code != http.StatusBadGateway {
			t.Fatalf("attempt %d: want 502, got %d", i, w.Code)
		}
	}

	rec.mu.Lock()
	rec.err = nil
	rec.reads = []anpr.Read{{Plate: "OK1", Confidence: 0.9}}
	rec.mu.Unlock()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, imageRequest(t, env, tok, []byte("x"), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("recovered call: want 200, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/anpr/health", nil)
	r.Header.Set("X-Tenant-Id", env.Tenant)
	r.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, `"state":"ok"`) {
		t.Fatalf("want state=ok after recovery, got %s", body)
	}
	if !strings.Contains(body, `"fail_streak":0`) {
		t.Fatalf("want streak=0 after recovery, got %s", body)
	}
}
