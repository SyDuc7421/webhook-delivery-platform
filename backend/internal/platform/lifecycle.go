package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

// Component is a long-running part of a process: an HTTP server, a consumer loop, a poller.
// Start blocks until the component stops or fails; Stop asks it to finish in-flight work
// and must return before ctx expires (NFR-R3).
type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Run starts every component and blocks until ctx is cancelled (normally SIGINT/SIGTERM via
// signal.NotifyContext in main) or one component fails. It then stops all components in
// reverse order within shutdownTimeout. The first component error is returned.
func Run(ctx context.Context, logger *slog.Logger, shutdownTimeout time.Duration, components ...Component) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, c := range components {
		g.Go(func() error {
			logger.InfoContext(gctx, "component starting", slog.String("component", c.Name()))
			if err := c.Start(gctx); err != nil {
				return fmt.Errorf("%s: %w", c.Name(), err)
			}
			return nil
		})
	}

	// Stop everything once the process is asked to stop or any component returns.
	stopped := make(chan error, 1)
	go func() {
		<-gctx.Done()
		logger.InfoContext(context.WithoutCancel(ctx), "shutting down", slog.Duration("timeout", shutdownTimeout))
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		var errs []error
		for i := len(components) - 1; i >= 0; i-- {
			c := components[i]
			if err := c.Stop(sctx); err != nil {
				errs = append(errs, fmt.Errorf("stop %s: %w", c.Name(), err))
			}
		}
		stopped <- errors.Join(errs...)
	}()

	runErr := g.Wait()
	stopErr := <-stopped
	if runErr != nil {
		return runErr
	}
	return stopErr
}

// Idle is a placeholder component for processes whose work lands in a later milestone.
// It only waits for shutdown, so the process can already boot, report health and be scraped.
type Idle struct {
	name   string
	until  string
	logger *slog.Logger
	done   chan struct{}
}

// NewIdle returns an Idle component named name that logs which milestone fills it in.
func NewIdle(name, until string, logger *slog.Logger) *Idle {
	return &Idle{name: name, until: until, logger: logger, done: make(chan struct{})}
}

// Name implements Component.
func (i *Idle) Name() string { return i.name }

// Start implements Component.
func (i *Idle) Start(ctx context.Context) error {
	i.logger.InfoContext(ctx, "component idle until its milestone", slog.String("component", i.name), slog.String("milestone", i.until))
	select {
	case <-ctx.Done():
	case <-i.done:
	}
	return nil
}

// Stop implements Component.
func (i *Idle) Stop(context.Context) error {
	select {
	case <-i.done:
	default:
		close(i.done)
	}
	return nil
}
