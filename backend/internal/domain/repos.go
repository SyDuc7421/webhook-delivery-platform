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
