// Command delivery-worker sends first delivery attempts from the deliveries topic.
package main

import (
	"context"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/cmd/internal/boot"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
)

func main() {
	boot.Main("delivery-worker", func(ctx context.Context, p *boot.Process) error {
		if _, err := p.Postgres(ctx); err != nil {
			return err
		}
		if _, err := p.Kafka(); err != nil {
			return err
		}
		p.Add(platform.NewIdle("delivery-worker", "M2", p.Logger))
		return nil
	})
}
