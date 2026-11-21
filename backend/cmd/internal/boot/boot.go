// Package boot holds the wiring shared by every cmd/<process>: config, logger, signals,
// ops server and dependency constructors. It lives under cmd/ so only composition roots
// can import it — no business logic here.
package boot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	goredis "github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/cache/redis"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/messaging/kafka"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
)

// Version is stamped at build time:
// -ldflags "-X github.com/SyDuc7421/webhook-delivery-platform/backend/cmd/internal/boot.Version=…".
// APP_VERSION overrides it when set to anything other than "dev".
var Version = "dev"

// Process is what a setup function receives and fills in.
type Process struct {
	Name     string
	Config   config.Config
	Logger   *slog.Logger
	Registry *prometheus.Registry

	components []platform.Component
	checks     []observability.Check
	closers    []func()
	health     *observability.Health
}

// Add registers long-running components, started in order and stopped in reverse.
func (p *Process) Add(c ...platform.Component) { p.components = append(p.components, c...) }

// Check registers a /readyz check.
func (p *Process) Check(c observability.Check) { p.checks = append(p.checks, c) }

// Health returns the process's health handlers, built from the checks registered so far.
// Call it after adding dependencies; the ops server reuses the same instance.
func (p *Process) Health() *observability.Health {
	if p.health == nil {
		p.health = observability.NewHealth(p.checks...)
	}
	return p.health
}

// OnClose registers cleanup run after all components stopped, in reverse order.
func (p *Process) OnClose(f func()) { p.closers = append(p.closers, f) }

// Postgres opens the pool (fails boot if Postgres is unreachable) and adds a critical check.
func (p *Process) Postgres(ctx context.Context) (*pgxpool.Pool, error) {
	pool, err := postgres.NewPool(ctx, p.Config.Postgres)
	if err != nil {
		return nil, err
	}
	p.OnClose(pool.Close)
	p.Check(observability.Check{Name: "postgres", Critical: true, Probe: pool.Ping})
	return pool, nil
}

// Redis creates the client and adds a non-critical check (NFR-D1: boot without Redis).
func (p *Process) Redis() *goredis.Client {
	c := redis.NewClient(p.Config.Redis)
	p.OnClose(func() { _ = c.Close() }) // close error at exit is not actionable
	p.Check(observability.Check{Name: "redis", Probe: func(ctx context.Context) error { return redis.Ping(ctx, c) }})
	return c
}

// Kafka creates a client and adds a critical check.
func (p *Process) Kafka(opts ...kgo.Opt) (*kgo.Client, error) {
	c, err := kafka.NewClient(p.Config.Kafka, opts...)
	if err != nil {
		return nil, err
	}
	p.OnClose(c.Close)
	p.Check(observability.Check{Name: "kafka", Critical: true, Probe: func(ctx context.Context) error { return kafka.Ping(ctx, c) }})
	return c, nil
}

// Main runs a process and exits. setup wires dependencies and components; it must not
// start goroutines itself.
func Main(name string, setup func(ctx context.Context, p *Process) error) {
	os.Exit(run(name, setup))
}

func run(name string, setup func(ctx context.Context, p *Process) error) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err) // logger not built yet
		return 2
	}
	if cfg.App.Version == "dev" {
		cfg.App.Version = Version
	}
	logger := observability.NewLogger(os.Stdout, name, cfg.App.Env, cfg.App.Version, cfg.App.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	p := &Process{Name: name, Config: cfg, Logger: logger, Registry: observability.NewRegistry()}
	defer func() {
		for i := len(p.closers) - 1; i >= 0; i-- {
			p.closers[i]()
		}
	}()

	if err := setup(ctx, p); err != nil {
		logger.ErrorContext(ctx, "setup failed", slog.String("error", err.Error()))
		return 1
	}

	ops := observability.NewOpsServer(cfg.Metrics.Addr, p.Registry, p.Health(), logger)
	components := append([]platform.Component{ops}, p.components...)
	if err := platform.Run(ctx, logger, cfg.App.ShutdownTimeout, components...); err != nil {
		logger.ErrorContext(ctx, "process stopped with error", slog.String("error", err.Error()))
		return 1
	}
	logger.InfoContext(ctx, "process stopped cleanly")
	return 0
}
