package domain

import (
	"context"

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
