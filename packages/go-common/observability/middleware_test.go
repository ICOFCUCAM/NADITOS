package observability_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/observability"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestMiddleware_GeneratesRequestId: a request without X-Request-Id
// gets a generated one in the response header and in ctx.
func TestMiddleware_GeneratesRequestId(t *testing.T) {
	var ctxRID string
	mw := observability.Middleware(discardLogger(), "svc")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			ctxRID, _, _ = observability.IDs(r.Context())
			w.WriteHeader(http.StatusOK)
		}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	hdr := rec.Header().Get("X-Request-Id")
	if hdr == "" || ctxRID == "" {
		t.Fatalf("rid empty: hdr=%q ctx=%q", hdr, ctxRID)
	}
	if hdr != ctxRID {
		t.Fatalf("rid mismatch: hdr=%q ctx=%q", hdr, ctxRID)
	}
}

// TestMiddleware_PropagatesIncomingRequestId: an incoming X-Request-Id
// is preserved through the response — critical for tracing a single
// user action across services.
func TestMiddleware_PropagatesIncomingRequestId(t *testing.T) {
	mw := observability.Middleware(discardLogger(), "svc")(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("X-Request-Id", "rid-from-caller")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, r)
	if got := rec.Header().Get("X-Request-Id"); got != "rid-from-caller" {
		t.Fatalf("propagation failed: %q", got)
	}
}

// TestMiddleware_RecordsHandlerStatus: the inner handler's WriteHeader
// status is what shows up in the access log + metrics — not the
// http.ResponseWriter's default 200. Without this, every error
// looks like a success in observability data.
func TestMiddleware_RecordsHandlerStatus(t *testing.T) {
	var got int
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	// Build a logger that captures records into a buffer so we can
	// assert the structured log used the right status.
	buf := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(textBufWriter{buf}, &slog.HandlerOptions{}))

	mw := observability.Middleware(logger, "svc")(probe)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/teapot", nil))
	got = rec.Code
	if got != http.StatusTeapot {
		t.Fatalf("status: %d", got)
	}
	if !strings.Contains(buf.String(), "status=418") {
		t.Fatalf("log missing status: %q", buf.String())
	}
}

// TestIDs_EmptyContext: IDs(ctx) on a vanilla context returns three
// empty strings (no panic). Handlers that aren't behind the
// middleware can call IDs safely.
func TestIDs_EmptyContext(t *testing.T) {
	r, tr, sp := observability.IDs(context.Background())
	if r != "" || tr != "" || sp != "" {
		t.Fatalf("empty ctx: %q %q %q", r, tr, sp)
	}
}

// TestMetricsHandler_PrometheusShape: /metrics renders Prometheus
// exposition with the request counter the middleware updates.
func TestMetricsHandler_PrometheusShape(t *testing.T) {
	// Drive at least one request through the middleware so the
	// counter has a value.
	mw := observability.Middleware(discardLogger(), "svc")(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/_", nil))

	rec := httptest.NewRecorder()
	observability.MetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type: %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "naditos_http_requests_total") {
		t.Fatalf("missing counter: %s", body)
	}
	if !strings.Contains(body, `naditos_http_responses_total{class="2xx"}`) {
		t.Fatalf("missing 2xx bucket: %s", body)
	}
}

type textBufWriter struct{ b *strings.Builder }

func (w textBufWriter) Write(p []byte) (int, error) { return w.b.Write(p) }
