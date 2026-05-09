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
	"github.com/icofcucam/naditos/packages/go-common/contracts/insurance"
	"github.com/icofcucam/naditos/packages/go-common/events"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
	"github.com/icofcucam/naditos/services/insurance/internal/worker"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// stubVerifier implements insurance.Verifier and records every call.
type stubVerifier struct {
	mu     sync.Mutex
	called []string
	err    error
	policy *insurance.Policy
}

func (s *stubVerifier) Info() contracts.AdapterInfo {
	return contracts.AdapterInfo{Module: "insurance", Provider: "stub"}
}
func (s *stubVerifier) VerifyByPlate(_ context.Context, _, plate string) (*insurance.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called = append(s.called, plate)
	if s.err != nil {
		return nil, s.err
	}
	return s.policy, nil
}
func (s *stubVerifier) VerifyByVIN(_ context.Context, _, _ string) (*insurance.Policy, error) {
	return nil, nil
}

func build(env *testkit.Env, stub insurance.Verifier) *worker.Worker {
	router := connectors.NewRouter[insurance.Verifier]()
	router.SetDefault(stub)
	return worker.New(env.AdminPool(), discardLogger(), router,
		connectors.NewHealthMonitor(env.AdminPool()),
		connectors.NewRetryQueue(env.AdminPool()),
		events.NewInProc(discardLogger()))
}

// waitForJobStatus polls retry_jobs and Fatals after deadline.
func waitForJobStatus(t *testing.T, env *testkit.Env, tenant, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		err := env.QueryRow(
			`SELECT status::text FROM retry_jobs
			  WHERE tenant_id=$1 AND module='insurance'
			  ORDER BY created_at DESC LIMIT 1`, tenant).Scan(&status)
		if err == nil && status == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("retry_jobs did not reach status=%q in time", want)
}

// TestWorker_DrainsAndPersists: a verify job whose stub returns a real
// policy must drain to status='done', stamp insurance_records, and
// flip vehicles.insurance_expires_at to the policy's expiry. This is
// the path that unblocks a vehicle from "red" once the bureau confirms
// active coverage.
func TestWorker_DrainsAndPersists(t *testing.T) {
	env := testkit.Setup(t)

	plate := "INS-12-34"
	vid := uuid.New()
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate)
	          VALUES ($1, $2, $3)`, vid, env.Tenant, plate)

	expires := time.Now().Add(180 * 24 * time.Hour).UTC().Truncate(time.Second)
	stub := &stubVerifier{
		policy: &insurance.Policy{
			Provider: "Bureau-X", PolicyNumber: "POL-001",
			StartsAt: time.Now().UTC().Add(-30 * 24 * time.Hour),
			ExpiresAt: expires, IsActive: true,
		},
	}
	w := build(env, stub)

	q := connectors.NewRetryQueue(env.AdminPool())
	if _, err := q.Enqueue(context.Background(), env.Tenant, "insurance", "verify",
		map[string]string{"plate": plate, "vehicle_id": vid.String()}, 5); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	waitForJobStatus(t, env, env.Tenant, "done")

	stub.mu.Lock()
	if len(stub.called) != 1 || stub.called[0] != plate {
		stub.mu.Unlock()
		t.Fatalf("verifier called=%v", stub.called)
	}
	stub.mu.Unlock()

	// insurance_records row was written + vehicle's expiry advanced.
	var n int
	if err := env.QueryRow(
		`SELECT count(*) FROM insurance_records
		  WHERE tenant_id=$1 AND vehicle_id=$2::uuid AND policy_number='POL-001'`,
		env.Tenant, vid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 insurance_records row, got %d", n)
	}
	var got time.Time
	if err := env.QueryRow(
		`SELECT insurance_expires_at FROM vehicles WHERE id=$1::uuid`,
		vid).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(expires) {
		t.Fatalf("insurance_expires_at: want %v got %v", expires, got)
	}
}

// TestWorker_FailureBacksOff: a failing provider increments attempts
// and reschedules the job. Mirrors the inspection worker test of the
// same name — proves the retry-queue path on the failure side.
func TestWorker_FailureBacksOff(t *testing.T) {
	env := testkit.Setup(t)
	stub := &stubVerifier{err: errors.New("upstream down")}
	w := build(env, stub)

	q := connectors.NewRetryQueue(env.AdminPool())
	if _, err := q.Enqueue(context.Background(), env.Tenant, "insurance", "verify",
		map[string]string{"plate": "AB-12-CD"}, 5); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var attempts int
		var status string
		_ = env.QueryRow(
			`SELECT attempts, status::text FROM retry_jobs
			  WHERE tenant_id=$1 AND module='insurance'
			  ORDER BY created_at DESC LIMIT 1`, env.Tenant).
			Scan(&attempts, &status)
		if attempts >= 1 && status == "queued" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("job did not get retried after failure")
}
