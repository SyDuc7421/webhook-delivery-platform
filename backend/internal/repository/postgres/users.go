package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres/db"
)

// OrgRepo implements domain.OrgRepository.
type OrgRepo struct{ pool *pgxpool.Pool }

// NewOrgRepo returns an OrgRepo.
func NewOrgRepo(pool *pgxpool.Pool) *OrgRepo { return &OrgRepo{pool: pool} }

// Create inserts an org.
func (r *OrgRepo) Create(ctx context.Context, o domain.Org) error {
	if err := queries(ctx, r.pool).CreateOrg(ctx, db.CreateOrgParams{ID: o.ID, Name: o.Name}); err != nil {
		return fmt.Errorf("create org: %w", mapErr(err))
	}
	return nil
}

// Get returns an org by id.
func (r *OrgRepo) Get(ctx context.Context, id uuid.UUID) (domain.Org, error) {
	row, err := queries(ctx, r.pool).GetOrg(ctx, id)
	if err != nil {
		return domain.Org{}, fmt.Errorf("get org %s: %w", id, mapErr(err))
	}
	return domain.Org{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt}, nil
}

// UserRepo implements domain.UserRepository.
type UserRepo struct{ pool *pgxpool.Pool }

// NewUserRepo returns a UserRepo.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo { return &UserRepo{pool: pool} }

// Create inserts a user.
func (r *UserRepo) Create(ctx context.Context, u domain.User) error {
	err := queries(ctx, r.pool).CreateUser(ctx, db.CreateUserParams{
		ID: u.ID, OrgID: u.OrgID, Email: u.Email, PasswordHash: u.PasswordHash, Role: string(u.Role),
	})
	if err != nil {
		return fmt.Errorf("create user: %w", mapErr(err))
	}
	return nil
}

// GetByEmail returns the user with this exact email.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := queries(ctx, r.pool).GetUserByEmail(ctx, email)
	if err != nil {
		return domain.User{}, fmt.Errorf("get user by email: %w", mapErr(err))
	}
	return userFromRow(row.ID, row.OrgID, row.Email, row.PasswordHash, row.Role, row.CreatedAt), nil
}

// Get returns a user by id.
func (r *UserRepo) Get(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := queries(ctx, r.pool).GetUser(ctx, id)
	if err != nil {
		return domain.User{}, fmt.Errorf("get user %s: %w", id, mapErr(err))
	}
	return userFromRow(row.ID, row.OrgID, row.Email, row.PasswordHash, row.Role, row.CreatedAt), nil
}

func userFromRow(id, orgID uuid.UUID, email, hash, role string, createdAt time.Time) domain.User {
	return domain.User{ID: id, OrgID: orgID, Email: email, PasswordHash: hash, Role: domain.Role(role), CreatedAt: createdAt}
}
