// Command relay moves outbox rows to the Kafka events topic (ADR-0002).
package main

import (
	"context"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/cmd/internal/boot"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/app"
)

func main() {
	boot.Main("relay", func(ctx context.Context, p *boot.Process) error {
		pool, err := p.Postgres(ctx)
		if err != nil {
			return err
		}
		kc, err := p.Kafka()
		if err != nil {
			return err
		}
		relay, err := app.NewRelayComponent(app.RelayDeps{
			Config: p.Config, Pool: pool, Kafka: kc, Logger: p.Logger, Registry: p.Registry,
		})
		if err != nil {
			return err
		}
		p.Add(relay)
		return nil
	})
}
