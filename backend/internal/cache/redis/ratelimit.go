package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

// rateLimitScript is a fixed-window counter. It runs atomically so no key is ever left
// without a TTL, and returns {count, remaining ttl in ms}.
var rateLimitScript = goredis.NewScript(`
local n = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if n == 1 or ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}
`)

// RateLimiter implements domain.RateLimiter with one Redis counter per org per window.
type RateLimiter struct {
	c      *goredis.Client
	limit  int
	window time.Duration
}

// NewRateLimiter allows limit requests per window for each org.
func NewRateLimiter(c *goredis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{c: c, limit: limit, window: window}
}

// Allow counts one request for the org. Over the limit it reports how long until the window
// resets. Redis errors are wrapped in domain.ErrUnavailable so callers can fail open.
func (r *RateLimiter) Allow(ctx context.Context, orgID uuid.UUID) (bool, time.Duration, error) {
	res, err := rateLimitScript.Run(ctx, r.c, []string{OrgRateLimitKey(orgID.String())}, r.window.Milliseconds()).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("rate limit: %w: %w", domain.ErrUnavailable, err)
	}
	if len(res) != 2 {
		return false, 0, fmt.Errorf("rate limit: %w: unexpected script result %v", domain.ErrUnavailable, res)
	}
	if res[0] <= int64(r.limit) {
		return true, 0, nil
	}
	return false, time.Duration(res[1]) * time.Millisecond, nil
}
