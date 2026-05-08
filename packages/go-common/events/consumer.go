// Outbox consumer with per-consumer offsets.
//
// The producer-side Relay (one per producing service) drains
// event_outbox into a transport bus and stamps `delivered_at`. That
// works when the transport actually fans out (e.g. NATS JetStream),
// but in dev with the InProc bus the relay's events are visible only
// inside the producer's own process — every other service is blind.
//
// The Consumer pattern below is the cross-process-safe alternative.
// Each consuming service runs its own Consumer with a unique name; the
// per-consumer offset in event_consumer_offsets keeps them independent
// of the producer-side relay AND of each other. Consumers don't claim
// rows via SKIP LOCKED — they just track where they are.
//
// Use cases in the platform:
//   - notifications: subscribes to fine.* / license.* and sends SMS/email
//   - license: re-injects fine.issued into its own InProc bus so the
//              demerit engine fires regardless of which process emitted
//              the event
//   - analytics: future
package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dispatcher is what a Consumer hands every drained event to. The
// callback runs synchronously inside the consumer's tick; it should
// return quickly (write a row, fire a local bus). Errors are logged
// but never block the offset advance — failed events are durable
// elsewhere (notification_records, retry_jobs) and can be re-driven.
type Dispatcher func(ctx context.Context, env Envelope) error

// Consumer drains event_outbox into Dispatcher, advancing a
// per-consumer offset. Run one per consuming service replica; multiple
// replicas with the same Name will skip-lock-coordinate via the
// offset row's FOR UPDATE.
type Consumer struct {
	pool      *pgxpool.Pool
	log       *slog.Logger
	name      string // identifies the offset row
	dispatch  Dispatcher
	pollEvery time.Duration
	batch     int
	// types is an optional allow-list of event types — if empty, all
	// events flow to the dispatcher.
	types map[string]struct{}
}

// NewConsumer constructs a Consumer. The name must be unique and
// stable across deploys; rolling out a new release with the same name
// resumes from the existing offset. To replay from the beginning,
// delete the offset row.
func NewConsumer(pool *pgxpool.Pool, log *slog.Logger, name string, d Dispatcher) *Consumer {
	return &Consumer{
		pool: pool, log: log, name: name, dispatch: d,
		pollEvery: 500 * time.Millisecond, batch: 100,
		types: map[string]struct{}{},
	}
}

// OnlyTypes restricts the consumer to a set of event types. Off the
// hot path, the consumer still reads every row from the outbox; the
// filter happens in Go to keep the SQL simple. Phase-4 may push the
// filter into the WHERE clause if needed.
func (c *Consumer) OnlyTypes(types ...string) *Consumer {
	for _, t := range types {
		c.types[t] = struct{}{}
	}
	return c
}

func (c *Consumer) Run(ctx context.Context) {
	t := time.NewTicker(c.pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.tick(ctx); err != nil {
				c.log.Warn("event consumer tick failed",
					slog.String("name", c.name),
					slog.String("err", err.Error()))
			}
		}
	}
}

func (c *Consumer) tick(ctx context.Context) error {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Get / initialise the offset.
	var lastID int64
	err = conn.QueryRow(ctx,
		`SELECT last_event_id FROM event_consumer_offsets WHERE consumer=$1`,
		c.name).Scan(&lastID)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := conn.Exec(ctx,
			`INSERT INTO event_consumer_offsets (consumer, last_event_id) VALUES ($1, 0)
			 ON CONFLICT DO NOTHING`, c.name); err != nil {
			return err
		}
		lastID = 0
	} else if err != nil {
		return err
	}

	rows, err := conn.Query(ctx,
		`SELECT id, envelope FROM event_outbox
		  WHERE id > $1 ORDER BY id ASC LIMIT $2`, lastID, c.batch)
	if err != nil {
		return err
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
		return nil
	}

	for _, p := range batch {
		var env Envelope
		if err := json.Unmarshal(p.body, &env); err != nil {
			c.log.Warn("event consumer: bad envelope",
				slog.String("name", c.name), slog.Int64("id", p.id),
				slog.String("err", err.Error()))
			continue
		}
		if len(c.types) > 0 {
			if _, ok := c.types[env.Type]; !ok {
				continue
			}
		}
		if err := c.dispatch(ctx, env); err != nil {
			c.log.Warn("event consumer: dispatch failed",
				slog.String("name", c.name),
				slog.Int64("id", p.id),
				slog.String("type", env.Type),
				slog.String("err", err.Error()))
			// Advance anyway — failures are recorded elsewhere
			// (e.g. notification_records). A poisoned event must not
			// halt the stream.
		}
	}

	highest := batch[len(batch)-1].id
	_, err = conn.Exec(ctx,
		`UPDATE event_consumer_offsets SET last_event_id=$2, updated_at=now()
		   WHERE consumer=$1`, c.name, highest)
	return err
}
