package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
)

// NewClient returns a franz-go client. kgo's defaults already give acks=all and idempotent
// production, which the pipeline relies on (docs/MESSAGING.md §Producing). Extra options
// (consumer group, topics) are added by each process.
func NewClient(cfg config.Kafka, opts ...kgo.Opt) (*kgo.Client, error) {
	all := append([]kgo.Opt{kgo.SeedBrokers(cfg.Brokers...)}, opts...)
	cl, err := kgo.NewClient(all...)
	if err != nil {
		return nil, fmt.Errorf("new kafka client: %w", err)
	}
	return cl, nil
}

// Ping reports whether at least one broker answers; used by /readyz.
func Ping(ctx context.Context, cl *kgo.Client) error {
	if err := cl.Ping(ctx); err != nil {
		return fmt.Errorf("ping kafka: %w", err)
	}
	return nil
}
