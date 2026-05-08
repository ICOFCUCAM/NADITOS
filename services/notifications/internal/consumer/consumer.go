// Package consumer drains event_outbox into notification_records.
//
// Every event the notifications service cares about is mapped to a
// renderer that produces (channel, recipient, subject, body). The
// renderer also resolves the recipient by reading vehicle ownership
// and citizen contact info from the registry tables.
//
// The consumer maintains a single offset per consumer-name in
// event_consumer_offsets — independent of the fines/registry relays
// that drive cross-service publication. This means one row in
// event_outbox can be processed by multiple consumers (relay + this).
//
// Idempotency: notification_records is the durable record. We INSERT
// before sending; if the send fails we update status='failed' but the
// row stays. Re-running over the same event would create a duplicate
// row, so the consumer always advances its offset PAST the event id
// even on send failure — failed sends are visible in the records and
// can be retried via a separate reaper.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/icofcucam/naditos/packages/go-common/contracts/notifications"
	"github.com/icofcucam/naditos/packages/go-common/events"
)

const consumerName = "notifications"

type Consumer struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	send notifications.Sender
	pollEvery time.Duration
	batch     int
}

func New(pool *pgxpool.Pool, log *slog.Logger, sender notifications.Sender) *Consumer {
	return &Consumer{
		pool: pool, log: log, send: sender,
		pollEvery: 500 * time.Millisecond, batch: 50,
	}
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
				c.log.Warn("notifications consumer tick failed", "err", err)
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

	// 1. Read the current offset.
	var lastID int64
	err = conn.QueryRow(ctx,
		`SELECT last_event_id FROM event_consumer_offsets WHERE consumer=$1`,
		consumerName).Scan(&lastID)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := conn.Exec(ctx,
			`INSERT INTO event_consumer_offsets (consumer, last_event_id) VALUES ($1, 0)
			 ON CONFLICT DO NOTHING`,
			consumerName); err != nil {
			return err
		}
		lastID = 0
	} else if err != nil {
		return err
	}

	// 2. Read the batch (no tx — we'll start a fresh one per event).
	rows, err := conn.Query(ctx,
		`SELECT id, envelope FROM event_outbox
		  WHERE id > $1 ORDER BY id ASC LIMIT $2`,
		lastID, c.batch)
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

	// 3. Process each event in its OWN transaction. A poisoned event
	//    (FK violation, missing tenant, malformed envelope) doesn't
	//    abort the batch — its tx rolls back, the next event gets a
	//    fresh tx, and the offset still advances past it.
	for _, p := range batch {
		var env events.Envelope
		if err := json.Unmarshal(p.body, &env); err != nil {
			c.log.Warn("notifications: bad envelope", "id", p.id, "err", err)
			continue
		}
		if err := c.handleEventInTx(ctx, conn, p.id, env); err != nil {
			c.log.Warn("notifications: handler failed",
				slog.Int64("event_id", p.id),
				slog.String("type", env.Type),
				slog.String("err", err.Error()))
		}
	}

	// 4. Advance offset past the highest id we saw, even if some
	//    sends failed — failures are durable in notification_records
	//    and Phase-4 retries via a separate reaper.
	highest := batch[len(batch)-1].id
	if _, err := conn.Exec(ctx,
		`UPDATE event_consumer_offsets
		    SET last_event_id=$2, updated_at=now()
		  WHERE consumer=$1`, consumerName, highest); err != nil {
		return err
	}
	return nil
}

// handleEventInTx wraps one event's handling in its own transaction.
// Per-event isolation keeps one poisoned event (e.g. FK violation
// against a torn-down test tenant) from aborting sibling events in
// the same batch.
func (c *Consumer) handleEventInTx(ctx context.Context, conn *pgxpool.Conn, eventID int64, env events.Envelope) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := c.handleEvent(ctx, tx, eventID, env); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// handleEvent dispatches by type and returns the first error in the
// pipeline (resolve → render → send). We keep the work inside the same
// tx as the offset update so a worker crash doesn't lose progress.
func (c *Consumer) handleEvent(ctx context.Context, tx pgx.Tx, eventID int64, env events.Envelope) error {
	r, ok := renderers[env.Type]
	if !ok {
		return nil // not interesting to notifications
	}

	// Resolve recipient.
	rec, err := r.resolve(ctx, tx, env)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	if rec == nil {
		// No contact info known; record as suppressed for visibility.
		if _, err := tx.Exec(ctx,
			`INSERT INTO notification_records
			   (tenant_id, related_event, channel, recipient, body,
			    template, status)
			 VALUES ($1, $2, 'email', '', '(no recipient resolvable)',
			         $3, 'suppressed')`,
			env.TenantID, eventID, r.template); err != nil {
			return fmt.Errorf("insert suppressed: %w", err)
		}
		return nil
	}

	subject, body := r.render(env, rec)

	// Insert pending row first — durable record before we attempt send.
	var notifID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO notification_records
		   (tenant_id, related_event, channel, recipient, subject, body, template, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending')
		 RETURNING id`,
		env.TenantID, eventID, rec.Channel, rec.Address, subject, body, r.template).
		Scan(&notifID); err != nil {
		return fmt.Errorf("insert notification_records: %w", err)
	}

	// Send via the configured provider.
	//
	// Send failures are RECORDED, not propagated: the row stays in
	// notification_records with status='failed', and we return nil so
	// the per-event tx commits and the consumer advances its offset.
	// Reaping failed rows for retry is Phase-4 work.
	receipt, err := c.send.Send(ctx, notifications.Message{
		TenantID:   env.TenantID,
		Channel:    notifications.Channel(rec.Channel),
		To:         rec.Address,
		Subject:    subject,
		Body:       body,
		TemplateID: r.template,
	})
	if err != nil {
		if _, uerr := tx.Exec(ctx,
			`UPDATE notification_records
			    SET status='failed', last_error=$2, attempts=attempts+1
			  WHERE id=$1`, notifID, err.Error()); uerr != nil {
			return fmt.Errorf("mark failed: %w", uerr)
		}
		c.log.Warn("notifications: send failed (recorded)",
			slog.String("provider", c.send.Info().Provider),
			slog.String("err", err.Error()))
		return nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE notification_records
		    SET status='sent', sent_at=now(),
		        provider=$2, provider_ref=$3, attempts=attempts+1
		  WHERE id=$1`, notifID, c.send.Info().Provider, receipt.ID); err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}
	return nil
}
