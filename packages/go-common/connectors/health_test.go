package connectors_test

import (
	"context"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/connectors"
	"github.com/icofcucam/naditos/packages/go-common/testkit"
)

// TestHealthMonitor_OKResetsStreak: a green call lands as state=ok
// with streak=0 even after prior failures had pushed it into
// degraded/down.
func TestHealthMonitor_OKResetsStreak(t *testing.T) {
	env := testkit.Setup(t)
	hm := connectors.NewHealthMonitor(env.AdminPool())
	ctx := context.Background()

	// Two failures push to degraded, streak=2.
	for i := 0; i < 2; i++ {
		if err := hm.Fail(ctx, env.Tenant, "insurance", "stub", "EU", "boom"); err != nil {
			t.Fatal(err)
		}
	}
	state, _, _, streak, err := hm.Snapshot(ctx, env.Tenant, "insurance", "stub")
	if err != nil {
		t.Fatal(err)
	}
	if state != connectors.HealthDegraded || streak != 2 {
		t.Fatalf("after 2 fails: state=%s streak=%d", state, streak)
	}

	// One OK resets cleanly.
	if err := hm.OK(ctx, env.Tenant, "insurance", "stub", "EU", nil); err != nil {
		t.Fatal(err)
	}
	state, lastOK, _, streak, err := hm.Snapshot(ctx, env.Tenant, "insurance", "stub")
	if err != nil {
		t.Fatal(err)
	}
	if state != connectors.HealthOK || streak != 0 {
		t.Fatalf("after OK: state=%s streak=%d", state, streak)
	}
	if lastOK.IsZero() {
		t.Fatal("last_ok_at should be set after OK")
	}
}

// TestHealthMonitor_FailStreakCrossesDownThreshold: state flips to
// "down" once streak reaches 5. A sixth failure stays at down.
func TestHealthMonitor_FailStreakCrossesDownThreshold(t *testing.T) {
	env := testkit.Setup(t)
	hm := connectors.NewHealthMonitor(env.AdminPool())
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		_ = hm.Fail(ctx, env.Tenant, "anpr", "stub", "", "x")
	}
	// 4 failures → still degraded.
	state, _, _, streak, _ := hm.Snapshot(ctx, env.Tenant, "anpr", "stub")
	if state != connectors.HealthDegraded || streak != 4 {
		t.Fatalf("after 4 fails: state=%s streak=%d", state, streak)
	}

	// 5th failure → down.
	_ = hm.Fail(ctx, env.Tenant, "anpr", "stub", "", "x")
	state, _, _, streak, _ = hm.Snapshot(ctx, env.Tenant, "anpr", "stub")
	if state != connectors.HealthDown || streak != 5 {
		t.Fatalf("after 5 fails: state=%s streak=%d", state, streak)
	}

	// 6th failure → still down, streak keeps incrementing.
	_ = hm.Fail(ctx, env.Tenant, "anpr", "stub", "", "x")
	state, _, _, streak, _ = hm.Snapshot(ctx, env.Tenant, "anpr", "stub")
	if state != connectors.HealthDown || streak != 6 {
		t.Fatalf("after 6 fails: state=%s streak=%d", state, streak)
	}
}

// TestHealthMonitor_PerProviderIndependence: two providers under the
// same module share no state — A's failure doesn't taint B.
func TestHealthMonitor_PerProviderIndependence(t *testing.T) {
	env := testkit.Setup(t)
	hm := connectors.NewHealthMonitor(env.AdminPool())
	ctx := context.Background()

	_ = hm.Fail(ctx, env.Tenant, "insurance", "provider-a", "", "down")
	_ = hm.OK(ctx, env.Tenant, "insurance", "provider-b", "", nil)

	stateA, _, _, streakA, _ := hm.Snapshot(ctx, env.Tenant, "insurance", "provider-a")
	if stateA != connectors.HealthDegraded || streakA != 1 {
		t.Fatalf("provider-a: state=%s streak=%d", stateA, streakA)
	}
	stateB, _, _, streakB, _ := hm.Snapshot(ctx, env.Tenant, "insurance", "provider-b")
	if stateB != connectors.HealthOK || streakB != 0 {
		t.Fatalf("provider-b: state=%s streak=%d", stateB, streakB)
	}
}
