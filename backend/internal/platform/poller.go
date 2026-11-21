package platform

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Poller runs fn immediately and then every interval until Stop. An error from fn is logged
// and the loop carries on: a dependency outage must not crash the process, and the next tick
// retries the same work.
type Poller struct {
	name     string
	interval time.Duration
	fn       func(ctx context.Context) error
	logger   *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewPoller returns a Component that polls fn.
func NewPoller(name string, interval time.Duration, fn func(ctx context.Context) error, logger *slog.Logger) *Poller {
	return &Poller{name: name, interval: interval, fn: fn, logger: logger, done: make(chan struct{})}
}

// Name implements Component.
func (p *Poller) Name() string { return p.name }

// Start implements Component. It blocks until Stop is called or ctx is cancelled.
func (p *Poller) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()
	defer close(p.done)
	defer cancel()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		if err := p.fn(ctx); err != nil && ctx.Err() == nil {
			p.logger.WarnContext(ctx, "poll failed, will retry", slog.String("component", p.name), slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Stop implements Component: it cancels the loop and waits for a running fn to return, or
// for ctx to expire.
func (p *Poller) Stop(ctx context.Context) error {
	p.mu.Lock()
	cancel := p.cancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop %s: %w", p.name, ctx.Err())
	}
}
