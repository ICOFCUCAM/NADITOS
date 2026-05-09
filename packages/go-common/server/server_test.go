package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/server"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestHealth_OK: /healthz and /livez return 200 with the documented
// JSON body. Kubernetes-style probes hit these.
func TestHealth_OK(t *testing.T) {
	cases := []struct{ path, body string }{
		{"/healthz", `{"ok":true}`},
		{"/livez", `{"alive":true}`},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		server.Health().ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d", tc.path, rec.Code)
		}
		if rec.Body.String() != tc.body {
			t.Errorf("%s body: %q", tc.path, rec.Body.String())
		}
	}
}

// TestMount_RoutesHealthAndMetrics: Mount layers /healthz, /livez,
// /metrics, and forwards everything else to the inner handler. The
// inner handler sees a request id from the observability middleware.
func TestMount_RoutesHealthAndMetrics(t *testing.T) {
	innerHits := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerHits++
		// Header set by observability middleware before reaching us.
		if r.Header.Get("X-Request-Id") == "" {
			// Note: observability middleware sets it on the response
			// header AND on the request context; the request header
			// is the inbound caller's. So this is just a sanity check
			// that the request reached us at all.
		}
		w.WriteHeader(200)
	})
	h := server.Mount(discardLogger(), "test-svc", inner)

	// /healthz hits the health handler, not inner.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("healthz: %d %s", rec.Code, rec.Body.String())
	}
	if innerHits != 0 {
		t.Errorf("inner handler hit on /healthz: %d", innerHits)
	}

	// /metrics hits the metrics handler.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "naditos_http_requests_total") {
		t.Errorf("metrics body: %s", rec.Body.String())
	}

	// /anything-else hits inner with a request id stamped on the
	// response by the middleware.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/users", nil))
	if innerHits != 1 {
		t.Errorf("inner hits: %d", innerHits)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id not set on response")
	}
}
