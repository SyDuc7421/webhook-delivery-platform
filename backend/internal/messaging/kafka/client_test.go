package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
)

func TestPingReportsUnreachableKafka(t *testing.T) {
	t.Parallel()
	cl, err := NewClient(config.Kafka{Brokers: []string{"127.0.0.1:1"}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := Ping(ctx, cl); err == nil {
		t.Fatal("expected error for unreachable kafka")
	}
}
