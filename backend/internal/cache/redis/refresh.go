package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

// RefreshStore implements domain.RefreshStore on Redis.
type RefreshStore struct {
	c *goredis.Client
}

// NewRefreshStore returns a RefreshStore.
func NewRefreshStore(c *goredis.Client) *RefreshStore { return &RefreshStore{c: c} }

// Save stores the token hash → user id with a TTL.
func (s *RefreshStore) Save(ctx context.Context, tokenHash string, userID uuid.UUID, ttl time.Duration) error {
	if err := s.c.Set(ctx, RefreshTokenKey(tokenHash), userID.String(), ttl).Err(); err != nil {
		return fmt.Errorf("save refresh token: %w: %w", domain.ErrUnavailable, err)
	}
	return nil
}

// Consume returns the user id and deletes the token in one GETDEL, so a token works once.
func (s *RefreshStore) Consume(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	v, err := s.c.GetDel(ctx, RefreshTokenKey(tokenHash)).Result()
	if errors.Is(err, goredis.Nil) {
		return uuid.Nil, fmt.Errorf("refresh token: %w", domain.ErrUnauthorized)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("consume refresh token: %w: %w", domain.ErrUnavailable, err)
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, fmt.Errorf("refresh token value: %w: %w", domain.ErrUnauthorized, err)
	}
	return id, nil
}

// Delete removes the token (logout). Deleting a missing token is not an error.
func (s *RefreshStore) Delete(ctx context.Context, tokenHash string) error {
	if err := s.c.Del(ctx, RefreshTokenKey(tokenHash)).Err(); err != nil {
		return fmt.Errorf("delete refresh token: %w: %w", domain.ErrUnavailable, err)
	}
	return nil
}
