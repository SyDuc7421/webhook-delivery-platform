package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
)

// NewClient returns a Redis client. It does not fail when Redis is down: Redis is
// loss-tolerant (NFR-D1), so callers degrade instead of refusing to boot.
func NewClient(cfg config.Redis) *goredis.Client {
	return goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})
}

// Ping reports whether Redis answers; used by /readyz as a non-critical check.
func Ping(ctx context.Context, c *goredis.Client) error {
	if err := c.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}
