//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/cache/redis"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/messaging/kafka"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/testenv"
)

// TestM0Skeleton proves the plumbing every later milestone builds on: migrations are
// reversible, every adapter reaches its real dependency, and /readyz keeps the process
// ready when Redis dies (NFR-D1).
func TestM0Skeleton(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("migrations are reversible", func(t *testing.T) {
		if err := env.MigrateDown(); err != nil {
			t.Fatalf("down: %v", err)
		}
		if err := env.MigrateUp(); err != nil {
			t.Fatalf("up again: %v", err)
		}
	})

	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("postgres pool: %v", err)
	}
	defer pool.Close()

	t.Run("pg_stat_statements installed", func(t *testing.T) {
		var n int
		if qerr := pool.QueryRow(ctx, `SELECT count(*) FROM pg_extension WHERE extname = 'pg_stat_statements'`).Scan(&n); qerr != nil {
			t.Fatalf("query: %v", qerr)
		}
		if n != 1 {
			t.Fatalf("extension count = %d, want 1", n)
		}
	})

	rc := redis.NewClient(env.Config.Redis)
	defer func() { _ = rc.Close() }()
	kc, err := kafka.NewClient(env.Config.Kafka)
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}
	defer kc.Close()

	health := observability.NewHealth(
		observability.Check{Name: "postgres", Critical: true, Probe: pool.Ping},
		observability.Check{Name: "kafka", Critical: true, Probe: func(ctx context.Context) error { return kafka.Ping(ctx, kc) }},
		observability.Check{Name: "redis", Probe: func(ctx context.Context) error { return redis.Ping(ctx, rc) }},
	)

	t.Run("all dependencies up", func(t *testing.T) {
		code, body := ready(ctx, t, health)
		if code != http.StatusOK {
			t.Fatalf("readyz = %d %+v", code, body)
		}
		for _, name := range []string{"postgres", "kafka", "redis"} {
			if body.Checks[name].Status != "up" {
				t.Errorf("%s = %+v", name, body.Checks[name])
			}
		}
	})

	t.Run("redis down keeps the process ready", func(t *testing.T) {
		stopCtx, stopCancel := context.WithTimeout(ctx, 30*time.Second)
		defer stopCancel()
		timeout := 5 * time.Second
		if err := env.Redis.Stop(stopCtx, &timeout); err != nil {
			t.Fatalf("stop redis: %v", err)
		}
		code, body := ready(ctx, t, health)
		if code != http.StatusOK {
			t.Fatalf("readyz = %d, want 200 with redis down (NFR-D1)", code)
		}
		if body.Checks["redis"].Status != "down" {
			t.Errorf("redis check = %+v, want down", body.Checks["redis"])
		}
	})
}

type readyBody struct {
	Status string `json:"status"`
	Checks map[string]struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"checks"`
}

func ready(ctx context.Context, t *testing.T, h *observability.Health) (int, readyBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/readyz", nil))
	var b readyBody
	if err := json.NewDecoder(rec.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rec.Code, b
}
