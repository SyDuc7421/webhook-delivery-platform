package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/messaging/kafka"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

// RelayDeps are the relay process's connected dependencies.
type RelayDeps struct {
	Config   config.Config
	Pool     *pgxpool.Pool
	Kafka    *kgo.Client
	Logger   *slog.Logger
	Registry *prometheus.Registry // receives webhook_outbox_backlog
}

// NewRelayComponent builds the outbox relay: it polls the outbox and, while batches come back
// full, drains without waiting for the next tick.
func NewRelayComponent(d RelayDeps) (platform.Component, error) {
	backlog := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "webhook_outbox_backlog", Help: "Outbox rows waiting to be published to Kafka.",
	})
	if err := d.Registry.Register(backlog); err != nil {
		return nil, fmt.Errorf("register outbox backlog gauge: %w", err)
	}
	relay := service.NewRelay(
		postgres.NewTransactor(d.Pool), postgres.NewOutboxRepo(d.Pool),
		kafka.NewEventProducer(d.Kafka, d.Config.Kafka.TopicPrefix), d.Config.Outbox.BatchSize, backlog)

	batch := d.Config.Outbox.BatchSize
	drain := func(ctx context.Context) error {
		// The gauge is refreshed on the way (throttled) and always when the run ends or fails.
		defer relay.RefreshBacklog(context.WithoutCancel(ctx), true)
		for ctx.Err() == nil {
			moved, err := relay.RunOnce(ctx)
			if err != nil {
				return err
			}
			if moved < batch {
				return nil // outbox is (nearly) empty; wait for the next tick
			}
			relay.RefreshBacklog(ctx, false)
		}
		return nil
	}
	return platform.NewPoller("relay", d.Config.Outbox.PollInterval, drain, d.Logger), nil
}
