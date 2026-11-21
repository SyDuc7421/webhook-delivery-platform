// Command retry-worker sends retries per tier and moves exhausted messages to the DLQ.
package main

import (
	"context"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/cmd/internal/boot"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
)

func main() {
	boot.Main("retry-worker", func(ctx context.Context, p *boot.Process) error {
		if _, err := p.Postgres(ctx); err != nil {
			return err
		}
		if _, err := p.Kafka(); err != nil {
			return err
		}
		p.Add(platform.NewIdle("retry-worker", "M2", p.Logger))
		return nil
	})
}
