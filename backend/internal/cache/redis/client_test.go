package redis

import (
	"context"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
)

func TestPingReportsUnreachableRedis(t *testing.T) {
	t.Parallel()
	c := NewClient(config.Redis{Addr: "127.0.0.1:1", PoolSize: 1})
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := Ping(ctx, c); err == nil {
		t.Fatal("expected error for unreachable redis")
	}
}
