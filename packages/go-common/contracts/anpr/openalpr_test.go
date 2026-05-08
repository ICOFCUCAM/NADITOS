package anpr_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/contracts/anpr"
)

// envelope mirrors the OpenALPR Cloud API v3 response shape so the tests
// build realistic payloads.
type envelope struct {
	Results []result `json:"results"`
	Error   string   `json:"error,omitempty"`
}
type result struct {
	Plate       string  `json:"plate"`
	Confidence  float64 `json:"confidence"`
	Region      string  `json:"region,omitempty"`
	Coordinates []point `json:"coordinates,omitempty"`
}
type point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func mockServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

// TestOpenALPR_HappyPath: server returns one detection; adapter parses
// it, normalizes confidence to 0..1, fills the BBox, and returns it.
func TestOpenALPR_HappyPath(t *testing.T) {
	var capturedSecret, capturedCountry, capturedImage string
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v3/recognize_bytes") {
			t.Errorf("path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Errorf("ct: %s", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		capturedSecret = r.FormValue("secret_key")
		capturedCountry = r.FormValue("country")
		capturedImage = r.FormValue("image_bytes")

		_ = json.NewEncoder(w).Encode(envelope{Results: []result{{
			Plate: "ABC123", Confidence: 92.3, Region: "us-ca",
			Coordinates: []point{{10, 20}, {100, 22}, {99, 50}, {12, 48}},
		}}})
	})

	o := &anpr.OpenALPR{
		BaseURL: srv.URL, SecretKey: "key123", Country: "us", HTTPClient: cli,
	}
	reads, err := o.Recognize(context.Background(),
		anpr.Image{Bytes: []byte("fake-jpg-bytes"), ContentType: "image/jpeg"},
		anpr.RecognizeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if capturedSecret != "key123" || capturedCountry != "us" {
		t.Fatalf("form: secret=%q country=%q", capturedSecret, capturedCountry)
	}
	got, _ := base64.StdEncoding.DecodeString(capturedImage)
	if string(got) != "fake-jpg-bytes" {
		t.Fatalf("image_bytes round-trip: %q", string(got))
	}
	if len(reads) != 1 {
		t.Fatalf("want 1 read, got %d", len(reads))
	}
	r := reads[0]
	if r.Plate != "ABC123" || r.Region != "us-ca" {
		t.Fatalf("plate/region: %+v", r)
	}
	if r.Confidence < 0.92 || r.Confidence > 0.93 {
		t.Fatalf("confidence not rescaled: %f", r.Confidence)
	}
	if r.BBox == nil {
		t.Fatal("bbox missing")
	}
	if r.BBox.X != 10 || r.BBox.Y != 20 || r.BBox.W != 90 || r.BBox.H != 30 {
		t.Fatalf("bbox: %+v", r.BBox)
	}
}

// TestOpenALPR_PerCallCountryWins: opts.Country overrides the adapter
// default. Per-tenant routing relies on this.
func TestOpenALPR_PerCallCountryWins(t *testing.T) {
	var seen string
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seen = r.FormValue("country")
		_ = json.NewEncoder(w).Encode(envelope{})
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", Country: "us", HTTPClient: cli}
	_, _ = o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{Country: "fr"})
	if seen != "fr" {
		t.Fatalf("country: want fr (override) got %q", seen)
	}
}

// TestOpenALPR_NoDetection: empty results array returns (nil, nil), not an error.
func TestOpenALPR_NoDetection(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(envelope{Results: []result{}})
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", HTTPClient: cli}
	reads, err := o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reads) != 0 {
		t.Fatalf("want 0 reads, got %d", len(reads))
	}
}

// TestOpenALPR_SortsByConfidence: multiple detections come back sorted high→low.
func TestOpenALPR_SortsByConfidence(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(envelope{Results: []result{
			{Plate: "LOW", Confidence: 60.0},
			{Plate: "HIGH", Confidence: 95.0},
			{Plate: "MID", Confidence: 80.0},
		}})
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", HTTPClient: cli}
	reads, err := o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reads) != 3 {
		t.Fatalf("want 3 reads, got %d", len(reads))
	}
	if reads[0].Plate != "HIGH" || reads[2].Plate != "LOW" {
		t.Fatalf("sort order: %v %v %v", reads[0].Plate, reads[1].Plate, reads[2].Plate)
	}
}

// TestOpenALPR_MinConfFilter: detections below opts.MinConf are dropped.
func TestOpenALPR_MinConfFilter(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(envelope{Results: []result{
			{Plate: "HIGH", Confidence: 95.0},
			{Plate: "LOW", Confidence: 35.0},
		}})
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", HTTPClient: cli}
	reads, err := o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")},
		anpr.RecognizeOpts{MinConf: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(reads) != 1 {
		t.Fatalf("want 1 read above MinConf, got %d", len(reads))
	}
	if reads[0].Plate != "HIGH" {
		t.Fatalf("plate: %s", reads[0].Plate)
	}
}

// TestOpenALPR_Unauthorized: 401 from upstream maps to ErrUnauthorized
// so HealthMonitor / retry logic can react to it specifically.
func TestOpenALPR_Unauthorized(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "bad", HTTPClient: cli}
	_, err := o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{})
	if !errors.Is(err, anpr.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

// TestOpenALPR_ServerError_Wraps: 5xx errors carry the body so ops can
// see the upstream's reason.
func TestOpenALPR_ServerError_Wraps(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`upstream blew up`))
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", HTTPClient: cli}
	_, err := o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "blew up") {
		t.Fatalf("err missing status/body: %v", err)
	}
}

// TestOpenALPR_RateLimited: 429 surfaces the body unchanged.
func TestOpenALPR_RateLimited(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota exceeded; retry in 60s"}`))
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", HTTPClient: cli}
	_, err := o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err: %v", err)
	}
}

// TestOpenALPR_ContextCancelled: a tight client deadline aborts the
// request before the server replies — proves we honor cancellation.
//
// The server responds after 1s; the client's ctx fires at 50ms. We
// also write a response when the request context is done so srv.Close()
// doesn't hang waiting for the keep-alive connection to drain.
func TestOpenALPR_ContextCancelled(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(1 * time.Second):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case <-r.Context().Done():
			// Client gave up; let the server return so the conn closes.
			return
		}
	})
	// Disable keep-alive on this client so srv.Close() returns promptly
	// after the test goroutine cancels.
	cli.Transport = &http.Transport{DisableKeepAlives: true}

	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", HTTPClient: cli}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := o.Recognize(ctx, anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{})
	if err == nil {
		t.Fatal("want error from cancellation")
	}
}

// TestOpenALPR_BadJSON: a non-JSON body wraps the snippet so ops can
// see what came back.
func TestOpenALPR_BadJSON(t *testing.T) {
	srv, cli := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><body>maintenance window</body>`))
	})
	o := &anpr.OpenALPR{BaseURL: srv.URL, SecretKey: "k", HTTPClient: cli}
	_, err := o.Recognize(context.Background(), anpr.Image{Bytes: []byte("x")}, anpr.RecognizeOpts{})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err: %v", err)
	}
}
