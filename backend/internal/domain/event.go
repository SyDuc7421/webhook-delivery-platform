package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event is one thing that happened in the org's product, accepted by the ingest API (FR-I1).
// ConsumerUID and Type are copied from the consumer and event type so the record is
// self-contained for the relay.
type Event struct {
	ID             uuid.UUID
	OrgID          uuid.UUID
	ConsumerID     uuid.UUID
	EventTypeID    uuid.UUID
	ConsumerUID    string
	Type           string
	Payload        json.RawMessage
	IdempotencyKey *string
	Traceparent    string
	CreatedAt      time.Time
}

// OutboxRecord is a pending outbox row joined with its event.
type OutboxRecord struct {
	OutboxID int64
	Event    Event
}
