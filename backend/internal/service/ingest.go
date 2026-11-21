package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
)

const maxIdempotencyKeyLength = 255

// IngestService accepts events (FR-I1–I4). It never touches Kafka: the event and its outbox
// row are committed together and the relay publishes later (ADR-0002).
type IngestService struct {
	tx         domain.Transactor
	events     domain.EventRepository
	outbox     domain.OutboxRepository
	consumers  domain.ConsumerRepository
	types      domain.EventTypeRepository
	maxPayload int
	clock      platform.Clock
	ingested   *prometheus.CounterVec
}

// NewIngestService returns an IngestService. ingested is labelled by result
// (accepted, duplicate, rejected, rate_limited).
func NewIngestService(tx domain.Transactor, events domain.EventRepository, outbox domain.OutboxRepository,
	consumers domain.ConsumerRepository, types domain.EventTypeRepository, maxPayload int,
	clock platform.Clock, ingested *prometheus.CounterVec,
) *IngestService {
	return &IngestService{
		tx: tx, events: events, outbox: outbox, consumers: consumers, types: types,
		maxPayload: maxPayload, clock: clock, ingested: ingested,
	}
}

// IngestInput is one request to POST /v1/events.
type IngestInput struct {
	OrgID          uuid.UUID
	ConsumerUID    string
	Type           string
	Payload        json.RawMessage
	IdempotencyKey string
	Traceparent    string
}

// IngestResult names the stored event. Duplicate means the Idempotency-Key matched an earlier
// event and nothing new was created.
type IngestResult struct {
	EventID   uuid.UUID
	Duplicate bool
}

// Ingest validates and stores one event plus its outbox row in a single transaction.
func (s *IngestService) Ingest(ctx context.Context, in IngestInput) (IngestResult, error) {
	res, err := s.ingest(ctx, in)
	switch {
	case err == nil && res.Duplicate:
		s.ingested.WithLabelValues("duplicate").Inc()
	case err == nil:
		s.ingested.WithLabelValues("accepted").Inc()
	case errors.Is(err, domain.ErrInvalidInput), errors.Is(err, domain.ErrUnprocessable), errors.Is(err, domain.ErrPayloadTooBig):
		s.ingested.WithLabelValues("rejected").Inc()
	}
	return res, err
}

func (s *IngestService) ingest(ctx context.Context, in IngestInput) (IngestResult, error) {
	if err := s.validateShape(in); err != nil {
		return IngestResult{}, err
	}

	// A replay returns the original even if the consumer or event type changed meanwhile.
	if in.IdempotencyKey != "" {
		if prior, err := s.events.GetByIdempotencyKey(ctx, in.OrgID, in.IdempotencyKey); err == nil {
			return IngestResult{EventID: prior.ID, Duplicate: true}, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return IngestResult{}, fmt.Errorf("check idempotency key: %w", err)
		}
	}

	consumer, err := s.consumers.GetByUID(ctx, in.OrgID, in.ConsumerUID)
	if errors.Is(err, domain.ErrNotFound) {
		return IngestResult{}, unprocessable("consumer_uid", "unknown consumer: "+in.ConsumerUID)
	}
	if err != nil {
		return IngestResult{}, fmt.Errorf("find consumer: %w", err)
	}
	types, err := s.types.GetByNames(ctx, in.OrgID, []string{in.Type})
	if err != nil {
		return IngestResult{}, fmt.Errorf("find event type: %w", err)
	}
	if len(types) == 0 {
		return IngestResult{}, unprocessable("type", "unknown event type: "+in.Type)
	}

	ev := domain.Event{
		ID: platform.NewID(), OrgID: in.OrgID, ConsumerID: consumer.ID, EventTypeID: types[0].ID,
		ConsumerUID: consumer.UID, Type: types[0].Name, Payload: in.Payload,
		Traceparent: in.Traceparent, CreatedAt: s.clock.Now(),
	}
	if in.IdempotencyKey != "" {
		key := in.IdempotencyKey
		ev.IdempotencyKey = &key
	}

	var created bool
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var ierr error
		created, ierr = s.events.Insert(ctx, ev)
		if ierr != nil || !created {
			return ierr
		}
		return s.outbox.Enqueue(ctx, ev.ID)
	})
	if err != nil {
		return IngestResult{}, fmt.Errorf("store event: %w", err)
	}
	if created {
		return IngestResult{EventID: ev.ID}, nil
	}

	// Lost a race with a concurrent request carrying the same key: return the winner's event.
	prior, err := s.events.GetByIdempotencyKey(ctx, in.OrgID, in.IdempotencyKey)
	if err != nil {
		return IngestResult{}, fmt.Errorf("load duplicate event: %w", err)
	}
	return IngestResult{EventID: prior.ID, Duplicate: true}, nil
}

// validateShape checks everything that needs no database.
func (s *IngestService) validateShape(in IngestInput) error {
	if len(in.Payload) > s.maxPayload {
		return fmt.Errorf("payload is %d bytes, limit %d: %w", len(in.Payload), s.maxPayload, domain.ErrPayloadTooBig)
	}
	ve := &domain.ValidationError{}
	if len(in.Payload) == 0 || !json.Valid(in.Payload) || !strings.HasPrefix(strings.TrimSpace(string(in.Payload)), "{") {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "payload", Issue: "must be a JSON object"})
	}
	if in.IdempotencyKey != "" && !validIdempotencyKey(in.IdempotencyKey) {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "idempotency_key", Issue: "must be 1 to 255 visible ASCII characters"})
	}
	if len(ve.Fields) > 0 {
		return ve
	}
	return nil
}

// validIdempotencyKey allows 1–255 visible ASCII characters (no spaces or control characters).
func validIdempotencyKey(k string) bool {
	if len(k) == 0 || len(k) > maxIdempotencyKeyLength {
		return false
	}
	for i := range len(k) {
		if k[i] < 0x21 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

func unprocessable(field, issue string) error {
	return &domain.ValidationError{
		Cause:  domain.ErrUnprocessable,
		Fields: []domain.FieldError{{Field: field, Issue: issue}},
	}
}
