package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres/db"
)

type txKey struct{}

// Transactor implements domain.Transactor with pgx.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor returns a Transactor over pool.
func NewTransactor(pool *pgxpool.Pool) *Transactor { return &Transactor{pool: pool} }

// WithinTx runs fn in a READ COMMITTED transaction. A nested call reuses the outer transaction.
func (t *Transactor) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := t.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// queries returns sqlc queries bound to the transaction in ctx, or to the pool.
func queries(ctx context.Context, pool *pgxpool.Pool) *db.Queries {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return db.New(tx)
	}
	return db.New(pool)
}

// inTx reports whether ctx carries a transaction (queries that take row locks require one).
func inTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(pgx.Tx)
	return ok
}

// mapErr translates driver errors into domain sentinels.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %w", domain.ErrNotFound, err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", domain.ErrAlreadyExists, pgErr.ConstraintName)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", domain.ErrNotFound, pgErr.ConstraintName)
		case "23001": // restrict_violation: deleting a row that others still reference
			return fmt.Errorf("%w: %s", domain.ErrConflict, pgErr.ConstraintName)
		case "22021", "22P05": // text jsonb cannot store (invalid UTF-8, \u0000): the caller sent it
			return fmt.Errorf("%w: %s", domain.ErrInvalidInput, pgErr.Message)
		case "23514": // check_violation
			return fmt.Errorf("%w: %s", domain.ErrInvalidInput, pgErr.ConstraintName)
		}
	}
	return err
}

// notFoundIfNone turns "0 rows affected" into ErrNotFound. Every scoped update/delete uses it,
// so another org's id looks exactly like a missing one.
func notFoundIfNone(rows int64, what string) error {
	if rows == 0 {
		return fmt.Errorf("%s: %w", what, domain.ErrNotFound)
	}
	return nil
}
