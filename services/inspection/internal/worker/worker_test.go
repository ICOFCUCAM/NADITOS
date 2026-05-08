package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/contracts"
	"github.com/icofcucam/naditos/packages/go-common/contracts/inspection"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/inspection/internal/worker"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// stubVerifier implements inspection.Verifier and records every call.
type stubVerifier struct {
	mu       sync.Mutex
	called   []string
	err      error
	rec      *inspection.Record
}

func (s *stubVerifier) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "inspection", Provider: "stub"}
}
func (s *stubVerifier) VerifyByPlate(_ context.Context, _, plate string) (*inspection.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called = append(s.called, plate)
	if s.err != nil {
		return nil, s.err
	}
	return s.rec, nil
}
func (s *stubVerifier) VerifyByVIN(_ context.Context, _, _ string) (*inspection.Record, error) {
	return nil, nil
}

// build returns a Worker wired up to a fresh router pointing at the
// supplied stub.
func build(env *testkit.Env, stub inspection.Verifier) *worker.Worker {
	router := connectors.NewRouter[inspection.Verifier]()
	router.SetDefault(stub)
	return worker.New(env.AdminPool(), discardLogger(), router,
		connectors.NewHealthMonitor(env.AdminPool()),
		connectors.NewRetryQueue(env.AdminPool()))
}

// runTickUntilNoJobs drives the worker synchronously by repeatedly
// reading + processing jobs until the queue is empty for this tenant.
// We can't use Worker.Run (it's a 2-second-tick goroutine) without
// goroutine-leak hassle in tests; expose private tick via Drain.
//
// For now we just call Claim+Done directly since the worker's tick is
// simple enough to mirror.
func waitForJobStatus(t *testing.T, env *testkit.Env, tenant string, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		err := env.QueryRow(
			`SELECT status::text FROM retry_jobs
			  WHERE tenant_id=$1 AND module='inspection'
			  ORDER BY created_at DESC LIMIT 1`, tenant).Scan(&status)
		if err == nil && status == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("retry_jobs did not reach status=%q in time", want)
}

// TestWorker_DrainsJob: enqueueing a verify job and starting the
// worker results in the verifier being called and the job marked done.
func TestWorker_DrainsJob(t *testing.T) {
	env := testkit.Setup(t)
	stub := &stubVerifier{rec: &inspection.Record{Station: "TÜV-1", Result: "pass",
		PerformedAt: time.Now(), ExpiresAt: time.Now().Add(365 * 24 * time.Hour)}}
	w := build(env, stub)

	q := connectors.NewRetryQueue(env.AdminPool())
	if _, err := q.Enqueue(context.Background(), env.Tenant, "inspection", "verify",
		map[string]string{"plate": "AB-12-CD", "vehicle_id": uuid.NewString()}, 5); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	waitForJobStatus(t, env, env.Tenant, "done")

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.called) != 1 || stub.called[0] != "AB-12-CD" {
		t.Fatalf("verifier called=%v", stub.called)
	}
}

// TestWorker_FailureBacksOff: a failing provider increments attempts
// and reschedules the job for later (next_run_at advances).
func TestWorker_FailureBacksOff(t *testing.T) {
	env := testkit.Setup(t)
	stub := &stubVerifier{err: errors.New("upstream down")}
	w := build(env, stub)

	q := connectors.NewRetryQueue(env.AdminPool())
	if _, err := q.Enqueue(context.Background(), env.Tenant, "inspection", "verify",
		map[string]string{"plate": "AB-12-CD"}, 5); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// Wait until attempts > 0; the worker reschedules on failure so
	// status should remain queued (or running briefly) — never done.
	// Match by the unique tenant we're testing in, restricted to the
	// most recent row so older test fixtures don't satisfy the check.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var attempts int
		var status string
		_ = env.QueryRow(
			`SELECT attempts, status::text FROM retry_jobs
			  WHERE tenant_id=$1 AND module='inspection'
			  ORDER BY created_at DESC LIMIT 1`, env.Tenant).
			Scan(&attempts, &status)
		if attempts >= 1 && status == "queued" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("job did not get retried after failure")
}
