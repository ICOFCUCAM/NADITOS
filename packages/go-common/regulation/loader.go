package regulation

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Loader reads the active country pack for each tenant and refreshes
// every reloadEvery interval. Services call Loader.For(tenantID) on the
// hot path and get the current Pack without DB latency.
type Loader struct {
	pool        *pgxpool.Pool
	reloadEvery time.Duration

	mu     sync.RWMutex
	byTenant map[string]*Pack
	loaded   time.Time
}

func NewLoader(pool *pgxpool.Pool) *Loader {
	return &Loader{pool: pool, reloadEvery: 30 * time.Second, byTenant: map[string]*Pack{}}
}

// For returns the active pack for a tenant, or nil if none bound.
func (l *Loader) For(tenantID string) *Pack {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.byTenant[tenantID]
}

// Run blocks reloading until ctx is cancelled.
func (l *Loader) Run(ctx context.Context) {
	_ = l.reload(ctx) // initial load
	t := time.NewTicker(l.reloadEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = l.reload(ctx)
		}
	}
}

func (l *Loader) reload(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return err
	}
	rows, err := conn.Query(ctx,
		`SELECT tcp.tenant_id, cp.manifest
		   FROM tenant_country_pack tcp
		   JOIN country_packs cp ON cp.id = tcp.pack_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	next := map[string]*Pack{}
	for rows.Next() {
		var tenant string
		var body []byte
		if err := rows.Scan(&tenant, &body); err != nil {
			continue
		}
		p, err := ParseManifest(body)
		if err != nil {
			continue
		}
		next[tenant] = p
	}
	l.mu.Lock()
	l.byTenant = next
	l.loaded = time.Now()
	l.mu.Unlock()
	return nil
}
