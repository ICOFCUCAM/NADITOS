package proxy

import (
	"sync"

	"golang.org/x/time/rate"
)

// rateLimiter is a per-(tenant, prefix) token-bucket. Buckets are kept in
// memory; for multi-replica gateways move to Redis token buckets — the
// interface stays identical.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]*rate.Limiter{}}
}

// Allow consumes one token from the bucket keyed by (tenant, prefix).
// reqPerMin is the bucket's average rate; burst is reqPerMin/4 (min 5).
func (l *rateLimiter) Allow(tenant, prefix string, reqPerMin int) bool {
	if reqPerMin <= 0 {
		return true
	}
	if tenant == "" {
		tenant = "_anon"
	}
	key := tenant + "|" + prefix
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		burst := reqPerMin / 4
		if burst < 5 {
			burst = 5
		}
		b = rate.NewLimiter(rate.Limit(float64(reqPerMin)/60.0), burst)
		l.buckets[key] = b
	}
	l.mu.Unlock()
	return b.Allow()
}
