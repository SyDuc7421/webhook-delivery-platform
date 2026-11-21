//go:build integration

// Package testenv starts real Postgres, Redis and Kafka with testcontainers and returns a
// Config pointing at them. Containers are shared per test binary; tests isolate their data
// (unique topic prefixes, own rows).
package testenv

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // migrate driver "pgx5"
	_ "github.com/golang-migrate/migrate/v4/source/file"     // migrate source "file"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
)

// Images. Postgres and Redis match deploy/docker-compose.yml. The testcontainers Kafka module
// only drives Confluent images, so tests use confluent-local (Confluent 8.x = Apache Kafka 4.x, KRaft) while
// compose uses apache/kafka; the client protocol used here is the same.
const (
	PostgresImage = "postgres:19-alpine"
	RedisImage    = "redis:8.10-alpine"
	KafkaImage    = "confluentinc/confluent-local:8.4.1"
)

// Env is a running set of dependencies.
type Env struct {
	Config config.Config
	Redis  *tcredis.RedisContainer
	pg     *tcpostgres.PostgresContainer
}

// Start runs all containers, applies migrations and registers cleanup on t.
func Start(t *testing.T) *Env {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pg, err := tcpostgres.Run(ctx, PostgresImage,
		tcpostgres.WithDatabase("webhook_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	cleanup(t, pg)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	rd, err := tcredis.Run(ctx, RedisImage)
	cleanup(t, rd)
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	kc, err := tckafka.Run(ctx, KafkaImage, tckafka.WithClusterID("webhook-test"))
	cleanup(t, kc)
	if err != nil {
		t.Fatalf("start kafka: %v", err)
	}

	pgDSN, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres dsn: %v", err)
	}
	redisURL, err := rd.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	brokers, err := kc.Brokers(ctx)
	if err != nil {
		t.Fatalf("kafka brokers: %v", err)
	}

	cfg, err := buildConfig(pgDSN, redisURL, brokers)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	env := &Env{Config: cfg, Redis: rd, pg: pg}
	if err := env.MigrateUp(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return env
}

func cleanup(t *testing.T, c testcontainers.Container) {
	t.Helper()
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(c); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})
}

func buildConfig(pgDSN, redisURL string, brokers []string) (config.Config, error) {
	pu, err := url.Parse(pgDSN)
	if err != nil {
		return config.Config{}, fmt.Errorf("parse postgres dsn: %w", err)
	}
	pgPort, err := strconv.Atoi(pu.Port())
	if err != nil {
		return config.Config{}, fmt.Errorf("postgres port: %w", err)
	}
	pw, _ := pu.User.Password()
	ru, err := url.Parse(redisURL)
	if err != nil {
		return config.Config{}, fmt.Errorf("parse redis url: %w", err)
	}
	return config.Config{
		App: config.App{Env: "local", Version: "test", LogLevel: "debug", ShutdownTimeout: 5 * time.Second},
		Postgres: config.Postgres{
			Host: pu.Hostname(), Port: pgPort, User: pu.User.Username(), Password: pw,
			DB: pu.Path[1:], SSLMode: "disable", MaxConns: 5, MinConns: 0, MaxConnLifetime: time.Minute,
		},
		Redis: config.Redis{Addr: ru.Host, PoolSize: 5},
		Kafka: config.Kafka{Brokers: brokers},
		Auth: config.Auth{
			JWTSecret: []byte("integration-test-jwt-secret-32-bytes!"), AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour,
		},
		Secrets: config.Secrets{EncryptionKey: make([]byte, 32)},
		Ingest:  config.Ingest{MaxPayloadBytes: 262144, RateLimitPerMin: 600},
		Outbox:  config.Outbox{PollInterval: 50 * time.Millisecond, BatchSize: 100},
	}, nil
}

func (e *Env) migrator() (*migrate.Migrate, error) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	p := e.Config.Postgres
	dsn := fmt.Sprintf("pgx5://%s:%s@%s:%d/%s?sslmode=disable", p.User, p.Password, p.Host, p.Port, p.DB)
	m, err := migrate.New("file://"+dir, dsn)
	if err != nil {
		return nil, fmt.Errorf("new migrator: %w", err)
	}
	return m, nil
}

// MigrateUp applies every migration.
func (e *Env) MigrateUp() error { return e.run(func(m *migrate.Migrate) error { return m.Up() }) }

// MigrateDown rolls every migration back.
func (e *Env) MigrateDown() error { return e.run(func(m *migrate.Migrate) error { return m.Down() }) }

func (e *Env) run(f func(*migrate.Migrate) error) error {
	m, err := e.migrator()
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }() // close errors after a finished run are not actionable
	if err := f(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
