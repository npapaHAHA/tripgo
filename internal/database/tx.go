package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type txManager struct {
	pool *pgxpool.Pool
}

type executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txContextKey struct{}

func NewTxManager(pool *pgxpool.Pool) TxManager {
	return &txManager{pool: pool}
}

func (m *txManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := transactionFromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txCtx := context.WithValue(ctx, txContextKey{}, tx)

	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback(ctx)
			panic(recovered)
		}
	}()

	if err := fn(txCtx); err != nil {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
		}

		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func executorFromContext(ctx context.Context, pool *pgxpool.Pool) executor {
	if tx, ok := transactionFromContext(ctx); ok {
		return tx
	}

	return pool
}

func transactionFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	return tx, ok
}
