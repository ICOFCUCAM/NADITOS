package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WriteOutbox persists an envelope into event_outbox inside the caller's
// transaction. The relay running elsewhere in the process drains the
// table and forwards to the real bus; producers therefore enjoy at-
// least-once delivery atomic with their domain write.
//
// Usage:
//
//	tx, _ := conn.Begin(ctx)
//	defer tx.Rollback(ctx)
//	// ... INSERTs / UPDATEs ...
//	if err := events.WriteOutbox(ctx, tx, env); err != nil { ... }
//	if err := tx.Commit(ctx); err != nil { ... }
//
// If the outbox INSERT fails the caller can roll back the whole change
// set; if it succeeds and Commit succeeds the event is durable. If the
// service crashes after Commit but before publish, the relay picks the
// row up on next tick.
func WriteOutbox(ctx context.Context, tx pgx.Tx, env Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO event_outbox (tenant_id, envelope) VALUES ($1, $2)`,
		env.TenantID, body)
	return err
}

// OutboxPublisher / NewOutbox / PublishTx are kept for callers that
// can't import pgx; the closure form is equivalent to WriteOutbox.
type OutboxPublisher struct{ pool *pgxpool.Pool }

func NewOutbox(pool *pgxpool.Pool) *OutboxPublisher { return &OutboxPublisher{pool: pool} }

func (o *OutboxPublisher) PublishTx(ctx context.Context, env Envelope,
	exec func(ctx context.Context, sql string, args ...any) error) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return exec(ctx,
		`INSERT INTO event_outbox (tenant_id, envelope) VALUES ($1, $2)`,
		env.TenantID, body)
}

// Relay is the loop that forwards undelivered outbox rows to the real
// bus and marks them delivered. Run one per service replica; SKIP LOCKED
// keeps replicas from double-publishing the same row.
type Relay struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	bus  Publisher
	batch int
	pollEvery time.Duration
}

func NewRelay(pool *pgxpool.Pool, log *slog.Logger, bus Publisher) *Relay {
	return &Relay{pool: pool, log: log, bus: bus, batch: 100, pollEvery: 500 * time.Millisecond}
}

func (r *Relay) Run(ctx context.Context) {
	t := time.NewTicker(r.pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Relay) tick(ctx context.Context) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET LOCAL row_security = off"); err != nil {
		return
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`SELECT id, envelope FROM event_outbox
		  WHERE delivered_at IS NULL
		  ORDER BY created_at
		   FOR UPDATE SKIP LOCKED LIMIT $1`, r.batch)
	if err != nil {
		return
	}
	type pending struct {
		id   int64
		body []byte
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.body); err != nil {
			continue
		}
		batch = append(batch, p)
	}
	rows.Close()
	if len(batch) == 0 {
		return
	}
	for _, p := range batch {
		var env Envelope
		if err := json.Unmarshal(p.body, &env); err != nil {
			_, _ = tx.Exec(ctx,
				`UPDATE event_outbox SET attempts=attempts+1, last_error=$2 WHERE id=$1`,
				p.id, err.Error())
			continue
		}
		if err := r.bus.Publish(ctx, env); err != nil {
			_, _ = tx.Exec(ctx,
				`UPDATE event_outbox SET attempts=attempts+1, last_error=$2 WHERE id=$1`,
				p.id, err.Error())
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE event_outbox SET delivered_at=now() WHERE id=$1`, p.id); err != nil {
			return
		}
	}
	_ = tx.Commit(ctx)
}
