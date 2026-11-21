package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
)

// APIKeyService manages ingest API keys (FR-K1–K3).
type APIKeyService struct {
	repo  domain.APIKeyRepository
	clock platform.Clock
}

// NewAPIKeyService returns an APIKeyService.
func NewAPIKeyService(repo domain.APIKeyRepository, clock platform.Clock) *APIKeyService {
	return &APIKeyService{repo: repo, clock: clock}
}

// Create makes a key and returns it together with the full key text, which is shown once.
func (s *APIKeyService) Create(ctx context.Context, orgID, userID uuid.UUID, name string) (domain.APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return domain.APIKey{}, "", domain.Invalid("name", "must be 1 to 100 characters")
	}
	full, prefix, hash, err := authn.NewAPIKey()
	if err != nil {
		return domain.APIKey{}, "", err
	}
	k := domain.APIKey{ID: platform.NewID(), OrgID: orgID, Name: name, Prefix: prefix, CreatedAt: s.clock.Now()}
	if err := s.repo.Create(ctx, k, hash, userID); err != nil {
		return domain.APIKey{}, "", fmt.Errorf("create api key: %w", err)
	}
	return k, full, nil
}

// List returns one page of the org's keys and the cursor for the next page (nil on the last).
func (s *APIKeyService) List(ctx context.Context, orgID uuid.UUID, p domain.Page) ([]domain.APIKey, *domain.Cursor, error) {
	rows, err := s.repo.List(ctx, orgID, domain.Page{Limit: p.Limit + 1, After: p.After})
	if err != nil {
		return nil, nil, fmt.Errorf("list api keys: %w", err)
	}
	rows, next := trimPage(rows, p.Limit, func(k domain.APIKey) domain.Cursor {
		return domain.Cursor{CreatedAt: k.CreatedAt, ID: k.ID}
	})
	return rows, next, nil
}

// Revoke disables a key immediately (FR-K3).
func (s *APIKeyService) Revoke(ctx context.Context, orgID, id uuid.UUID) error {
	if err := s.repo.Revoke(ctx, orgID, id, s.clock.Now()); err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// Authenticate maps a full API key to its org. Unknown, revoked and malformed keys are all
// ErrUnauthorized, and malformed ones never reach the database.
func (s *APIKeyService) Authenticate(ctx context.Context, fullKey string) (uuid.UUID, error) {
	if !authn.LooksLikeAPIKey(fullKey) {
		return uuid.Nil, fmt.Errorf("malformed api key: %w", domain.ErrUnauthorized)
	}
	k, err := s.repo.GetActiveByHash(ctx, authn.HashAPIKey(fullKey))
	if errors.Is(err, domain.ErrNotFound) {
		return uuid.Nil, fmt.Errorf("unknown or revoked api key: %w", domain.ErrUnauthorized)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("authenticate api key: %w", err)
	}
	return k.OrgID, nil
}

// trimPage drops the extra row fetched to detect another page and builds the next cursor.
func trimPage[T any](rows []T, limit int, cursorOf func(T) domain.Cursor) ([]T, *domain.Cursor) {
	if len(rows) <= limit {
		return rows, nil
	}
	rows = rows[:limit]
	c := cursorOf(rows[limit-1])
	return rows, &c
}
