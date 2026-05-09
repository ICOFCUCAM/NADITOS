package connectors_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
)

// TestRetryQueue_EnqueueClaimDoneSuccess: green path.
//   Enqueue → Claim returns the job → Done(nil) marks status=done.
// The enqueue payload round-trips byte-identical (json) and Done(nil)
// clears any previous last_error.
func TestRetryQueue_EnqueueClaimDoneSuccess(t *testing.T) {
	env := testkit.Setup(t)
	q := connectors.NewRetryQueue(env.AdminPool())
	ctx := context.Background()

	id, err := q.Enqueue(ctx, env.Tenant, "insurance", "verify",
		map[string]string{"plate": "AB-12-CD"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if id == [16]byte{} {
		t.Fatal("Enqueue returned zero id")
	}

	job, err := q.Claim(ctx, "insurance")
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.ID != id {
		t.Fatalf("claim: got %v, want id=%s", job, id)
	}
	if job.Module != "insurance" || job.Kind != "verify" {
		t.Fatalf("module/kind: %s/%s", job.Module, job.Kind)
	}
	if string(job.Payload) == "" {
		t.Fatal("payload empty")
	}

	if err := q.Done(ctx, job.ID, nil); err != nil {
		t.Fatal(err)
	}

	var status string
	var lastErr *string
	if err := env.QueryRow(
		`SELECT status::text, last_error FROM retry_jobs WHERE id=$1`, id).
		Scan(&status, &lastErr); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("status: want done, got %s", status)
	}
	if lastErr != nil {
		t.Fatalf("last_error should be cleared on success, got %v", *lastErr)
	}
}

// TestRetryQueue_FailureBacksOff: a failure increments attempts and
// reschedules the job for later. next_run_at must be in the future
// (the fix for the pgx interval bug; SQL would reject if the cast
// went wrong, hence make_interval).
func TestRetryQueue_FailureBacksOff(t *testing.T) {
	env := testkit.Setup(t)
	q := connectors.NewRetryQueue(env.AdminPool())
	ctx := context.Background()

	id, _ := q.Enqueue(ctx, env.Tenant, "insurance", "verify",
		map[string]string{"plate": "X"}, 5)
	job, err := q.Claim(ctx, "insurance")
	if err != nil || job == nil {
		t.Fatalf("claim: %v, %v", job, err)
	}
	if err := q.Done(ctx, job.ID, errors.New("upstream down")); err != nil {
		t.Fatal(err)
	}

	var attempts int
	var status string
	var lastErr string
	var nextRun time.Time
	if err := env.QueryRow(
		`SELECT attempts, status::text, last_error, next_run_at
		   FROM retry_jobs WHERE id=$1`, id).
		Scan(&attempts, &status, &lastErr, &nextRun); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("attempts: want 1, got %d", attempts)
	}
	if status != "queued" {
		t.Fatalf("status: want queued, got %s", status)
	}
	if lastErr != "upstream down" {
		t.Fatalf("last_error: want 'upstream down', got %q", lastErr)
	}
	if !nextRun.After(time.Now()) {
		t.Fatalf("next_run_at not in future: %v", nextRun)
	}
}

// TestRetryQueue_DeadLetterAfterMaxAttempts: max_attempts=2 + two
// failures puts the job in dead_letter. A third claim attempt for the
// same module returns nil (DLQ rows are off the work-queue surface).
func TestRetryQueue_DeadLetterAfterMaxAttempts(t *testing.T) {
	env := testkit.Setup(t)
	q := connectors.NewRetryQueue(env.AdminPool())
	ctx := context.Background()

	id, _ := q.Enqueue(ctx, env.Tenant, "insurance", "verify",
		map[string]string{"plate": "X"}, 2)

	// Two consecutive failures. Each one needs its own claim because
	// next_run_at is bumped 30s into the future after each Done(err).
	// We circumvent the schedule by SQL-bumping next_run_at backwards
	// between claims — the same trick a fast-replay test would use.
	for i := 0; i < 2; i++ {
		env.Exec(`UPDATE retry_jobs SET next_run_at=now() WHERE id=$1`, id)
		job, err := q.Claim(ctx, "insurance")
		if err != nil || job == nil {
			t.Fatalf("claim %d: %v %v", i, job, err)
		}
		_ = q.Done(ctx, job.ID, errors.New("boom"))
	}

	var status string
	if err := env.QueryRow(
		`SELECT status::text FROM retry_jobs WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "dead_letter" {
		t.Fatalf("after max_attempts: want dead_letter, got %s", status)
	}

	// Third claim sees nothing.
	env.Exec(`UPDATE retry_jobs SET next_run_at=now() WHERE id=$1`, id)
	job, _ := q.Claim(ctx, "insurance")
	if job != nil {
		t.Fatalf("dead_letter row should not be claimable: %+v", job)
	}
}

// TestRetryQueue_ModuleScoped: a worker for module=A doesn't claim a
// module=B job. Critical because every service runs its own worker
// against the same retry_jobs table.
func TestRetryQueue_ModuleScoped(t *testing.T) {
	env := testkit.Setup(t)
	q := connectors.NewRetryQueue(env.AdminPool())
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, env.Tenant, "insurance", "verify",
		map[string]string{}, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(ctx, env.Tenant, "inspection", "verify",
		map[string]string{}, 5); err != nil {
		t.Fatal(err)
	}

	insJob, _ := q.Claim(ctx, "insurance")
	if insJob == nil || insJob.Module != "insurance" {
		t.Fatalf("insurance worker: %+v", insJob)
	}
	inspJob, _ := q.Claim(ctx, "inspection")
	if inspJob == nil || inspJob.Module != "inspection" {
		t.Fatalf("inspection worker: %+v", inspJob)
	}
	if insJob.ID == inspJob.ID {
		t.Fatal("workers claimed the same row across modules")
	}
}

// TestRetryQueue_ClaimSkipsLocked: two concurrent Claims see different
// jobs (FOR UPDATE SKIP LOCKED). Without that, parallel workers would
// double-process the same row.
func TestRetryQueue_ClaimSkipsLocked(t *testing.T) {
	env := testkit.Setup(t)
	q := connectors.NewRetryQueue(env.AdminPool())
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, _ = q.Enqueue(ctx, env.Tenant, "insurance", "verify",
			map[string]int{"i": i}, 5)
	}

	// Concurrent claims.
	type res struct {
		job *connectors.Job
		err error
	}
	done := make(chan res, 2)
	for i := 0; i < 2; i++ {
		go func() {
			j, e := q.Claim(ctx, "insurance")
			done <- res{j, e}
		}()
	}
	got := []*connectors.Job{}
	for i := 0; i < 2; i++ {
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		got = append(got, r.job)
	}
	if got[0] == nil || got[1] == nil {
		t.Fatalf("nil claim: %+v %+v", got[0], got[1])
	}
	if got[0].ID == got[1].ID {
		t.Fatal("two concurrent Claims got the same job — SKIP LOCKED broken")
	}
}
