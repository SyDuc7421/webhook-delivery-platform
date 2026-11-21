// Command relay moves outbox rows to the Kafka events topic (ADR-0002).
package main

import (
	"context"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/cmd/internal/boot"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
)

func main() {
	boot.Main("relay", func(ctx context.Context, p *boot.Process) error {
		if _, err := p.Postgres(ctx); err != nil {
			return err
		}
		if _, err := p.Kafka(); err != nil {
			return err
		}
		p.Add(platform.NewIdle("relay", "M1", p.Logger))
		return nil
	})
}
