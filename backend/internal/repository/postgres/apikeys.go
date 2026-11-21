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

// APIKeyRepo implements domain.APIKeyRepository.
type APIKeyRepo struct{ pool *pgxpool.Pool }

// NewAPIKeyRepo returns an APIKeyRepo.
func NewAPIKeyRepo(pool *pgxpool.Pool) *APIKeyRepo { return &APIKeyRepo{pool: pool} }

// Create stores the key's hash and prefix; the full key is never stored.
func (r *APIKeyRepo) Create(ctx context.Context, k domain.APIKey, hash []byte, createdBy uuid.UUID) error {
	err := queries(ctx, r.pool).CreateAPIKey(ctx, db.CreateAPIKeyParams{
		ID: k.ID, OrgID: k.OrgID, Name: k.Name, Prefix: k.Prefix, KeyHash: hash, CreatedBy: &createdBy,
	})
	if err != nil {
		return fmt.Errorf("create api key: %w", mapErr(err))
	}
	return nil
}

// List returns a page of the org's keys, newest first.
func (r *APIKeyRepo) List(ctx context.Context, orgID uuid.UUID, p domain.Page) ([]domain.APIKey, error) {
	q := queries(ctx, r.pool)
	var rows []db.ListAPIKeysRow
	var err error
	if p.After == nil {
		rows, err = q.ListAPIKeys(ctx, db.ListAPIKeysParams{OrgID: orgID, Limit: int32(p.Limit)}) //nolint:gosec // Limit is capped at 101 by the service
	} else {
		var after []db.ListAPIKeysAfterRow
		after, err = q.ListAPIKeysAfter(ctx, db.ListAPIKeysAfterParams{
			OrgID: orgID, AfterCreatedAt: p.After.CreatedAt, AfterID: p.After.ID, PageLimit: int32(p.Limit), //nolint:gosec // capped by the service
		})
		for _, a := range after {
			rows = append(rows, db.ListAPIKeysRow(a))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", mapErr(err))
	}
	out := make([]domain.APIKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiKeyFromRow(db.GetActiveAPIKeyByHashRow(row)))
	}
	return out, nil
}

// Revoke marks the key revoked. An unknown, foreign or already revoked key is ErrNotFound.
func (r *APIKeyRepo) Revoke(ctx context.Context, orgID, id uuid.UUID, at time.Time) error {
	n, err := queries(ctx, r.pool).RevokeAPIKey(ctx, db.RevokeAPIKeyParams{OrgID: orgID, ID: id, RevokedAt: &at})
	if err != nil {
		return fmt.Errorf("revoke api key: %w", mapErr(err))
	}
	return notFoundIfNone(n, "api key")
}

// GetActiveByHash finds a non-revoked key by the sha256 of the full key.
func (r *APIKeyRepo) GetActiveByHash(ctx context.Context, hash []byte) (domain.APIKey, error) {
	row, err := queries(ctx, r.pool).GetActiveAPIKeyByHash(ctx, hash)
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("get api key by hash: %w", mapErr(err))
	}
	return apiKeyFromRow(row), nil
}

func apiKeyFromRow(r db.GetActiveAPIKeyByHashRow) domain.APIKey {
	return domain.APIKey{ID: r.ID, OrgID: r.OrgID, Name: r.Name, Prefix: r.Prefix, CreatedAt: r.CreatedAt, RevokedAt: r.RevokedAt}
}
