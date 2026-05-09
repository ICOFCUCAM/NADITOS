// Package server boots a standard HTTP server with health probes,
// observability middleware, metrics, and graceful shutdown. Used by
// every Go service via server.Run(ctx, log, name, port, handler).
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/observability"
)

func Health() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"alive":true}`))
	})
	return mux
}

// Mount layers /healthz, /livez, /metrics, and the observability
// middleware onto a service handler. The middleware:
//   - injects X-Request-Id and a trace id into the context
//   - emits a structured access log per request
//   - increments the in-process Prometheus counters served at /metrics
func Mount(log *slog.Logger, service string, h http.Handler) http.Handler {
	wrapped := observability.Middleware(log, service)(recoverMiddleware(log, service)(h))
	mux := http.NewServeMux()
	mux.Handle("/healthz", Health())
	mux.Handle("/livez", Health())
	mux.Handle("/metrics", observability.MetricsHandler())
	mux.Handle("/", wrapped)
	return mux
}

// recoverMiddleware catches panics in downstream handlers, logs the
// recovered value with a stack trace and the request id, then returns a
// generic 500 to the client. Without this a panic crashes only the
// goroutine but leaves the connection hanging with no log line — which
// is exactly the symptom you get when the auth login flow returns an
// unexplained 500.
func recoverMiddleware(log *slog.Logger, service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				rid, _, _ := observability.IDs(r.Context())
				log.Error("panic recovered",
					slog.String("svc", service),
					slog.String("rid", rid),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code":"internal","message":"internal error"}`))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Run starts the service on the given port. service is used in access
// logs and metric labels; it should match the service name (auth,
// registry, fines, ...).
func Run(ctx context.Context, log *slog.Logger, service string, port int, h http.Handler) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           Mount(log, service, h),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-stopCh:
		log.Info("shutting down")
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
