package service

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

// publishTimeout bounds one Kafka publish, and so how long the relay holds its row locks.
const publishTimeout = 15 * time.Second

// Relay moves outbox rows to Kafka (ADR-0002). Several relays can run side by side because
// each locks its own batch with FOR UPDATE SKIP LOCKED.
type Relay struct {
	tx      domain.Transactor
	outbox  domain.OutboxRepository
	pub     domain.EventPublisher
	batch   int
	backlog prometheus.Gauge
}

// NewRelay returns a Relay that moves up to batch rows per RunOnce. backlog is set to the
// number of pending rows after every run.
func NewRelay(tx domain.Transactor, outbox domain.OutboxRepository, pub domain.EventPublisher, batch int, backlog prometheus.Gauge) *Relay {
	return &Relay{tx: tx, outbox: outbox, pub: pub, batch: batch, backlog: backlog}
}

// RunOnce locks a batch, publishes it, waits for the broker acks and deletes the rows, all in
// one transaction. If publishing fails the transaction rolls back and the rows stay (at
// worst a crash after the ack and before the commit publishes them twice: at-least-once).
// It returns how many rows were moved.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	moved := 0
	err := r.tx.WithinTx(ctx, func(ctx context.Context) error {
		batch, err := r.outbox.LockBatch(ctx, r.batch)
		if err != nil {
			return fmt.Errorf("lock outbox batch: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		events := make([]domain.Event, 0, len(batch))
		ids := make([]int64, 0, len(batch))
		for _, rec := range batch {
			events = append(events, rec.Event)
			ids = append(ids, rec.OutboxID)
		}
		pubCtx, cancel := context.WithTimeout(ctx, publishTimeout)
		defer cancel()
		if err := r.pub.PublishEvents(pubCtx, events); err != nil {
			return fmt.Errorf("publish %d events: %w", len(events), err)
		}
		if err := r.outbox.Delete(ctx, ids); err != nil {
			return fmt.Errorf("delete outbox rows: %w", err)
		}
		moved = len(ids)
		return nil
	})
	// Best effort, on failure too: a Kafka outage is exactly when the backlog must be visible,
	// and a failed count must never fail a run.
	if n, cerr := r.outbox.Backlog(ctx); cerr == nil {
		r.backlog.Set(float64(n))
	}
	if err != nil {
		return 0, err
	}
	return moved, nil
}
