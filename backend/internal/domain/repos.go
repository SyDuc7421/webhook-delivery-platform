package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Transactor runs fn in one database transaction. Repositories called with the ctx passed to
// fn join that transaction; fn returning an error rolls it back.
type Transactor interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// OrgRepository persists orgs.
type OrgRepository interface {
	Create(ctx context.Context, o Org) error
	Get(ctx context.Context, id uuid.UUID) (Org, error)
}

// UserRepository persists users. Emails are compared exactly; callers normalise them.
type UserRepository interface {
	Create(ctx context.Context, u User) error // duplicate email → ErrAlreadyExists
	GetByEmail(ctx context.Context, email string) (User, error)
	Get(ctx context.Context, id uuid.UUID) (User, error)
}

// RefreshStore keeps single-use refresh tokens, keyed by token hash (FR-A2).
// It is loss-tolerant: losing it only forces users to log in again.
type RefreshStore interface {
	Save(ctx context.Context, tokenHash string, userID uuid.UUID, ttl time.Duration) error
	// Consume atomically reads and deletes. Missing → ErrUnauthorized; store down → ErrUnavailable.
	Consume(ctx context.Context, tokenHash string) (uuid.UUID, error)
	Delete(ctx context.Context, tokenHash string) error
}

// APIKeyRepository persists API keys. Every method but GetActiveByHash is scoped by org.
type APIKeyRepository interface {
	Create(ctx context.Context, k APIKey, hash []byte, createdBy uuid.UUID) error
	List(ctx context.Context, orgID uuid.UUID, p Page) ([]APIKey, error)
	Revoke(ctx context.Context, orgID, id uuid.UUID, at time.Time) error // not found or already revoked → ErrNotFound
	GetActiveByHash(ctx context.Context, hash []byte) (APIKey, error)    // revoked keys are not found (FR-K3)
}

// EventTypeRepository persists event types, scoped by org. A row of another org is ErrNotFound.
type EventTypeRepository interface {
	Create(ctx context.Context, e EventType) error
	Get(ctx context.Context, orgID, id uuid.UUID) (EventType, error)
	GetByNames(ctx context.Context, orgID uuid.UUID, names []string) ([]EventType, error)
	List(ctx context.Context, orgID uuid.UUID, p Page) ([]EventType, error)
	Update(ctx context.Context, e EventType) error
	Delete(ctx context.Context, orgID, id uuid.UUID) error
}

// ConsumerRepository persists consumers, scoped by org.
type ConsumerRepository interface {
	Create(ctx context.Context, c Consumer) error
	Get(ctx context.Context, orgID, id uuid.UUID) (Consumer, error)
	GetByUID(ctx context.Context, orgID uuid.UUID, uid string) (Consumer, error)
	List(ctx context.Context, orgID uuid.UUID, p Page) ([]Consumer, error)
	Update(ctx context.Context, c Consumer) error
	Delete(ctx context.Context, orgID, id uuid.UUID) error
}

// EndpointRepository persists endpoints, scoped by org.
type EndpointRepository interface {
	Create(ctx context.Context, e Endpoint) error
	Get(ctx context.Context, orgID, id uuid.UUID) (Endpoint, error)
	ListByConsumer(ctx context.Context, orgID, consumerID uuid.UUID, p Page) ([]Endpoint, error)
	Update(ctx context.Context, e Endpoint) error
	UpdateSecret(ctx context.Context, orgID, id uuid.UUID, ciphertext []byte) error
	Delete(ctx context.Context, orgID, id uuid.UUID) error
}

// EventRepository persists accepted events.
type EventRepository interface {
	// Insert stores the event. created=false means an event with the same (org, idempotency key)
	// already exists and nothing was written (FR-I3).
	Insert(ctx context.Context, e Event) (created bool, err error)
	GetByIdempotencyKey(ctx context.Context, orgID uuid.UUID, key string) (Event, error)
}

// OutboxRepository is the transactional outbox (ADR-0002): rows are written in the same
// transaction as the event and drained by the relay.
type OutboxRepository interface {
	Enqueue(ctx context.Context, eventID uuid.UUID) error
	// LockBatch returns up to limit rows ordered by id, locked with FOR UPDATE SKIP LOCKED so
	// several relays can run side by side. It must run inside Transactor.WithinTx.
	LockBatch(ctx context.Context, limit int) ([]OutboxRecord, error)
	Delete(ctx context.Context, ids []int64) error
	Backlog(ctx context.Context) (int64, error)
}

// RateLimiter limits ingest per org, with the counter shared by every api instance (FR-I5).
// It is loss-tolerant: callers fail open when it returns an error (NFR-D1).
type RateLimiter interface {
	Allow(ctx context.Context, orgID uuid.UUID) (allowed bool, retryAfter time.Duration, err error)
}

// EventPublisher puts accepted events on the events topic. PublishEvents returns only after
// the broker acknowledged every event, or with an error and nothing assumed delivered.
type EventPublisher interface {
	PublishEvents(ctx context.Context, events []Event) error
}
