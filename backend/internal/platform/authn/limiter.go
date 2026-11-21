package authn

import (
	"context"
	"fmt"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

// Limiter bounds how many argon2id computations run at once. Each one allocates 64 MiB, and
// register/login are unauthenticated, so without a bound a burst of bogus logins can exhaust
// the api's memory (and take ingest down with it).
type Limiter struct {
	sem chan struct{}
}

// NewLimiter allows at most n concurrent calls; n < 1 is treated as 1.
func NewLimiter(n int) *Limiter {
	return &Limiter{sem: make(chan struct{}, max(1, n))}
}

// Do runs fn once a slot is free. If ctx ends while waiting it returns domain.ErrUnavailable
// without running fn.
func (l *Limiter) Do(ctx context.Context, fn func() error) error {
	select {
	case l.sem <- struct{}{}:
		defer func() { <-l.sem }()
		return fn()
	case <-ctx.Done():
		return fmt.Errorf("waiting for a hashing slot: %w: %w", domain.ErrUnavailable, ctx.Err())
	}
}
