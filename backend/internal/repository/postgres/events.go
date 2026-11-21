package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres/db"
)

// EventRepo implements domain.EventRepository.
type EventRepo struct{ pool *pgxpool.Pool }

// NewEventRepo returns an EventRepo.
func NewEventRepo(pool *pgxpool.Pool) *EventRepo { return &EventRepo{pool: pool} }

// Insert stores the event unless (org, idempotency key) already exists.
func (r *EventRepo) Insert(ctx context.Context, e domain.Event) (bool, error) {
	n, err := queries(ctx, r.pool).InsertEvent(ctx, db.InsertEventParams{
		ID: e.ID, OrgID: e.OrgID, ConsumerID: e.ConsumerID, EventTypeID: e.EventTypeID,
		ConsumerUid: e.ConsumerUID, Type: e.Type, Payload: e.Payload,
		IdempotencyKey: e.IdempotencyKey, Traceparent: optionalString(e.Traceparent),
	})
	if err != nil {
		return false, fmt.Errorf("insert event: %w", mapErr(err))
	}
	return n == 1, nil
}

// GetByIdempotencyKey returns the org's event stored under this key.
func (r *EventRepo) GetByIdempotencyKey(ctx context.Context, orgID uuid.UUID, key string) (domain.Event, error) {
	row, err := queries(ctx, r.pool).GetEventByIdempotencyKey(ctx, db.GetEventByIdempotencyKeyParams{OrgID: orgID, IdempotencyKey: &key})
	if err != nil {
		return domain.Event{}, fmt.Errorf("get event by idempotency key: %w", mapErr(err))
	}
	return eventFromModel(row), nil
}

func eventFromModel(m db.Event) domain.Event {
	return domain.Event{
		ID: m.ID, OrgID: m.OrgID, ConsumerID: m.ConsumerID, EventTypeID: m.EventTypeID,
		ConsumerUID: m.ConsumerUid, Type: m.Type, Payload: m.Payload,
		IdempotencyKey: m.IdempotencyKey, Traceparent: deref(m.Traceparent), CreatedAt: m.CreatedAt,
	}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// OutboxRepo implements domain.OutboxRepository.
type OutboxRepo struct{ pool *pgxpool.Pool }

// NewOutboxRepo returns an OutboxRepo.
func NewOutboxRepo(pool *pgxpool.Pool) *OutboxRepo { return &OutboxRepo{pool: pool} }

// Enqueue adds an outbox row; call it in the same transaction as the event insert.
func (r *OutboxRepo) Enqueue(ctx context.Context, eventID uuid.UUID) error {
	if err := queries(ctx, r.pool).EnqueueOutbox(ctx, eventID); err != nil {
		return fmt.Errorf("enqueue outbox: %w", mapErr(err))
	}
	return nil
}

// LockBatch locks and returns the oldest outbox rows that no other relay holds.
func (r *OutboxRepo) LockBatch(ctx context.Context, limit int) ([]domain.OutboxRecord, error) {
	if !inTx(ctx) {
		return nil, errors.New("outbox LockBatch must run inside a transaction")
	}
	rows, err := queries(ctx, r.pool).LockOutboxBatch(ctx, int32(limit)) //nolint:gosec // limit is config-validated (> 0, small)
	if err != nil {
		return nil, fmt.Errorf("lock outbox batch: %w", mapErr(err))
	}
	out := make([]domain.OutboxRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.OutboxRecord{OutboxID: row.OutboxID, Event: domain.Event{
			ID: row.ID, OrgID: row.OrgID, ConsumerID: row.ConsumerID, EventTypeID: row.EventTypeID,
			ConsumerUID: row.ConsumerUid, Type: row.Type, Payload: row.Payload,
			IdempotencyKey: row.IdempotencyKey, Traceparent: deref(row.Traceparent), CreatedAt: row.CreatedAt,
		}})
	}
	return out, nil
}

// Delete removes outbox rows once their events are on Kafka.
func (r *OutboxRepo) Delete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := queries(ctx, r.pool).DeleteOutboxRows(ctx, ids); err != nil {
		return fmt.Errorf("delete outbox rows: %w", mapErr(err))
	}
	return nil
}

// Backlog counts pending outbox rows (the key outbox-lag metric).
func (r *OutboxRepo) Backlog(ctx context.Context) (int64, error) {
	n, err := queries(ctx, r.pool).CountOutbox(ctx)
	if err != nil {
		return 0, fmt.Errorf("count outbox: %w", mapErr(err))
	}
	return n, nil
}
