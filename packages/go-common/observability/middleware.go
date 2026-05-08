// Package observability adds request-id propagation, structured access
// logs, and basic latency/error metrics to every Go service. The
// OpenTelemetry tracer and metrics provider are initialized once per
// process via Init(); the middleware then wraps any http.Handler.
//
// Phase-1 ships an OTel-shaped API without taking the full SDK as a
// dependency — it emits structured slog records with the trace-context
// keys OTel uses (trace_id, span_id) so a Phase-2 swap to the real
// SDK requires only changing the body of NewTracer/NewMeter.
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota + 1
	keyTraceID
	keySpanID
)

// IDs returns the request id, trace id, and span id from a context.
func IDs(ctx context.Context) (req, trace, span string) {
	if v, ok := ctx.Value(keyRequestID).(string); ok {
		req = v
	}
	if v, ok := ctx.Value(keyTraceID).(string); ok {
		trace = v
	}
	if v, ok := ctx.Value(keySpanID).(string); ok {
		span = v
	}
	return
}

// Middleware injects/propagates X-Request-Id, opens a trace context, and
// emits a structured access log on response.
func Middleware(log *slog.Logger, service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := r.Header.Get("X-Request-Id")
			if rid == "" {
				rid = randHex(8)
			}
			tid := r.Header.Get("Traceparent") // W3C; in real OTel we'd parse this
			if tid == "" {
				tid = randHex(16)
			}
			sid := randHex(8)

			ctx := context.WithValue(r.Context(), keyRequestID, rid)
			ctx = context.WithValue(ctx, keyTraceID, tid)
			ctx = context.WithValue(ctx, keySpanID, sid)
			r = r.WithContext(ctx)

			w.Header().Set("X-Request-Id", rid)
			w.Header().Set("X-Trace-Id", tid)

			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: 200}

			next.ServeHTTP(rw, r)

			dur := time.Since(start)
			incReq(service, r.URL.Path, rw.status, dur)

			log.Info("http",
				slog.String("svc", service),
				slog.String("rid", rid),
				slog.String("trace", tid),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Duration("dur", dur),
				slog.String("ip", clientIP(r)),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *responseWriter) WriteHeader(s int) {
	if !w.wrote {
		w.status = s
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(s)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

// ─── In-process counters (Prometheus-shaped) ────────────────────────────────
//
// Every service exposes /metrics returning a text body suitable for
// Prometheus scraping. The format is intentionally simple — a Phase-2
// upgrade to the OTel metrics SDK is a 30-line drop-in.

type counter struct {
	count    atomic.Int64
	totalNs  atomic.Int64
	statuses [6]atomic.Int64 // index = status/100
}

var (
	registryReqs counter
)

func incReq(_svc, _path string, status int, d time.Duration) {
	registryReqs.count.Add(1)
	registryReqs.totalNs.Add(d.Nanoseconds())
	bucket := status / 100
	if bucket >= 0 && bucket < 6 {
		registryReqs.statuses[bucket].Add(1)
	}
}

// MetricsHandler serves a tiny Prometheus exposition for the in-process
// counters. Mount at /metrics in addition to /healthz.
func MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		c := registryReqs.count.Load()
		t := registryReqs.totalNs.Load()
		fmt.Fprintf(w, "naditos_http_requests_total %d\n", c)
		fmt.Fprintf(w, "naditos_http_duration_seconds_sum %.6f\n", float64(t)/1e9)
		for i := range registryReqs.statuses {
			if v := registryReqs.statuses[i].Load(); v > 0 {
				fmt.Fprintf(w, `naditos_http_responses_total{class="%dxx"} %d`+"\n", i, v)
			}
		}
	})
}

// ─── helpers ────────────────────────────────────────────────────────────────
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		return h
	}
	return r.RemoteAddr
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
