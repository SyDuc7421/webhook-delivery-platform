package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
)

func TestNewPoolFailsFastWhenUnreachable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err := NewPool(ctx, config.Postgres{
		Host: "127.0.0.1", Port: 1, User: "u", Password: "p", DB: "d", SSLMode: "disable",
		MaxConns: 2, MinConns: 0, MaxConnLifetime: time.Minute,
	})
	if err == nil {
		t.Fatal("expected error for unreachable postgres")
	}
}
