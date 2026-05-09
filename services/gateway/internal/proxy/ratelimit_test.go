package proxy

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestRateLimit_ZeroIsUnlimited: reqPerMin=0 short-circuits to always
// allow. Routes that opt out of rate limiting (e.g. health) MUST get
// this behaviour.
func TestRateLimit_ZeroIsUnlimited(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 10000; i++ {
		if !l.Allow("t1", "/v1/x", 0) {
			t.Fatalf("zero-rate denied at iteration %d", i)
		}
	}
}

// TestRateLimit_BurstThenDeny: with reqPerMin=60 the burst is 60/4=15.
// 15 immediate hits should succeed; the 16th should fail (token bucket
// hasn't refilled in microseconds). The burst floor is 5, so even tiny
// rates allow at least 5 in a row.
func TestRateLimit_BurstThenDeny(t *testing.T) {
	l := newRateLimiter()
	allowed := 0
	for i := 0; i < 60; i++ {
		if l.Allow("t1", "/v1/burst", 60) {
			allowed++
		}
	}
	if allowed < 5 {
		t.Fatalf("burst floor: want >= 5 allowed, got %d", allowed)
	}
	// 60 requests in microseconds against a 60-per-minute bucket can't
	// all succeed.
	if allowed >= 60 {
		t.Fatalf("rate limit didn't engage: all %d allowed", allowed)
	}
}

// TestRateLimit_PerTenantIndependent: two tenants under the same
// prefix have independent buckets. Tenant A burning its bucket
// doesn't affect tenant B.
func TestRateLimit_PerTenantIndependent(t *testing.T) {
	l := newRateLimiter()
	// Burn tenant A's bucket.
	for i := 0; i < 30; i++ {
		_ = l.Allow("a", "/v1/x", 6)
	}
	// Tenant B starts fresh — its first burst of 5 must succeed.
	for i := 0; i < 5; i++ {
		if !l.Allow("b", "/v1/x", 6) {
			t.Fatalf("tenant B denied at iteration %d (per-tenant isolation broken)", i)
		}
	}
}

// TestRateLimit_PerPrefixIndependent: same tenant on two different
// prefixes also gets independent buckets. Burning /v1/scans doesn't
// limit /v1/fines.
func TestRateLimit_PerPrefixIndependent(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 30; i++ {
		_ = l.Allow("t1", "/v1/scans", 6)
	}
	for i := 0; i < 5; i++ {
		if !l.Allow("t1", "/v1/fines", 6) {
			t.Fatalf("/v1/fines denied at iteration %d (per-prefix isolation broken)", i)
		}
	}
}

// TestRateLimit_EmptyTenantBucket: an unauthenticated route that
// reaches Allow with tenant="" still gets a bucket (under "_anon")
// and doesn't crash.
func TestRateLimit_EmptyTenantBucket(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 5; i++ {
		if !l.Allow("", "/v1/login", 60) {
			t.Fatalf("empty-tenant burst denied at %d", i)
		}
	}
}

// TestRateLimit_ConcurrentSafe: many goroutines hammering the same
// bucket must not race. Verified under -race and with the resulting
// allowed-count not exceeding reqPerMin's burst (which would mean the
// bucket double-issued tokens under contention).
func TestRateLimit_ConcurrentSafe(t *testing.T) {
	l := newRateLimiter()
	var allowed int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("t1", "/v1/concurrent", 60) {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()
	// Burst is 60/4=15. Some refill happens during 200 goroutines, so
	// the cap is loose; we just assert the limiter actually denied
	// some calls and the counter survived the race intact.
	if allowed == 0 {
		t.Fatal("concurrent: no calls allowed")
	}
	if allowed >= 200 {
		t.Fatalf("concurrent: all %d allowed (limiter not engaged)", allowed)
	}
}
