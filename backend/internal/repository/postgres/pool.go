package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/config"
)

// NewPool opens a pgx pool sized from config and verifies the connection.
func NewPool(ctx context.Context, cfg config.Postgres) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	pcfg.MaxConns = int32(cfg.MaxConns) //nolint:gosec // bounded by config validation
	pcfg.MinConns = int32(cfg.MinConns) //nolint:gosec // bounded by config validation
	pcfg.MaxConnLifetime = cfg.MaxConnLifetime

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
