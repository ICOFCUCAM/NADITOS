package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxPublisher persists events to the event_outbox table inside the
// caller's transaction; a relay drains the table and forwards to the
// real bus. This is the gold-standard pattern for atomic
// "state-change-and-event" updates across services.
//
//   tx := db.Begin(...)
//   // ... domain mutations on the same tx ...
//   outbox.Publish(ctx, tx, env)
//   tx.Commit()
//
// A relay loop reads pending rows and replays them, marking delivered.
type OutboxPublisher struct {
	pool *pgxpool.Pool
}

func NewOutbox(pool *pgxpool.Pool) *OutboxPublisher {
	return &OutboxPublisher{pool: pool}
}

// PublishTx writes one envelope inside an existing transaction. Pass a
// closure that runs the INSERT against your tx — this avoids any direct
// pgx type leak in the events package while keeping the call atomic.
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
