//go:build integration

package trip_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/infra/postgres/transaction"
	repository "github.com/npapaHAHA/tripgo/internal/repository/trip"
)

func TestRepositoryLifecycle(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	value := integrationTrip()
	cleanupTrips(t, pool, value.ID)

	require.NoError(t, createWithHistory(ctx, tx, repo, value))
	fetched, err := repo.GetByID(ctx, value.ID)
	require.NoError(t, err)
	requireSameTrip(t, value, fetched)
	require.Equal(t, time.UTC, fetched.StartedAt.Location())

	var from *string
	var to, reason string
	err = pool.QueryRow(ctx,
		"SELECT from_status, to_status, reason FROM trip_status_history WHERE trip_id = $1",
		value.ID).Scan(&from, &to, &reason)
	require.NoError(t, err)
	require.Nil(t, from)
	require.Equal(t, string(trip.StatusActive), to)
	require.Equal(t, "trip created", reason)

	finishedAt := value.StartedAt.Add(time.Hour)
	var completed trip.Trip
	err = tx.Do(ctx, func(txCtx context.Context) error {
		updated, updateErr := repo.Finish(txCtx, value.ID, finishedAt)
		if updateErr != nil {
			return updateErr
		}
		completed = updated
		previous := trip.StatusActive
		return repo.AddStatusHistory(txCtx, value.ID, &previous, trip.StatusCompleted, "trip completed")
	})
	require.NoError(t, err)
	value.Status = trip.StatusCompleted
	value.FinishedAt = &finishedAt
	requireSameTrip(t, value, completed)
	require.Equal(t, time.UTC, completed.FinishedAt.Location())
	fetched, err = repo.GetByID(ctx, value.ID)
	require.NoError(t, err)
	requireSameTrip(t, value, fetched)

	var histories int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM trip_status_history WHERE trip_id = $1", value.ID).Scan(&histories))
	require.Equal(t, 2, histories)
	_, err = repo.Finish(ctx, value.ID, finishedAt.Add(time.Hour))
	require.ErrorIs(t, err, trip.ErrTripCompleted)
	_, err = repo.GetByID(ctx, uuid.New())
	require.ErrorIs(t, err, trip.ErrTripNotFound)
	_, err = repo.Finish(ctx, uuid.New(), finishedAt)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestRepositoryDriverBusy(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	first, second := integrationTrip(), integrationTrip()
	second.DriverID = first.DriverID
	cleanupTrips(t, pool, first.ID, second.ID)

	require.NoError(t, repo.Create(ctx, first))
	require.ErrorIs(t, repo.Create(ctx, second), trip.ErrDriverBusy)
	_, err := repo.GetByID(ctx, second.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
	_, err = repo.Finish(ctx, first.ID, first.StartedAt.Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, second), "a completed trip must release the driver")
}

func TestRepositoryConcurrentCreate(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	first, second := integrationTrip(), integrationTrip()
	second.DriverID = first.DriverID
	cleanupTrips(t, pool, first.ID, second.ID)

	values := []trip.Trip{first, second}
	start := make(chan struct{})
	results := make(chan error, len(values))
	var ready sync.WaitGroup
	ready.Add(len(values))
	for _, value := range values {
		go func() {
			ready.Done()
			<-start
			results <- createWithHistory(ctx, tx, repo, value)
		}()
	}
	ready.Wait()
	close(start)
	var succeeded, busy int
	for range values {
		err := <-results
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, trip.ErrDriverBusy)
			busy++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, busy)
	var trips, histories int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM trips WHERE id IN ($1, $2)", first.ID, second.ID).Scan(&trips))
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM trip_status_history WHERE trip_id IN ($1, $2)", first.ID, second.ID).Scan(&histories))
	require.Equal(t, 1, trips)
	require.Equal(t, 1, histories)
}

func TestRepositoryConcurrentFinish(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	value := integrationTrip()
	cleanupTrips(t, pool, value.ID)
	require.NoError(t, createWithHistory(ctx, tx, repo, value))

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			results <- tx.Do(ctx, func(txCtx context.Context) error {
				if _, err := repo.Finish(txCtx, value.ID, time.Now().UTC()); err != nil {
					return err
				}
				previous := trip.StatusActive
				return repo.AddStatusHistory(txCtx, value.ID, &previous, trip.StatusCompleted, "trip completed")
			})
		}()
	}
	ready.Wait()
	close(start)
	var succeeded, conflicts int
	for range 2 {
		err := <-results
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, trip.ErrTripCompleted)
			conflicts++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicts)
	completed, err := repo.GetByID(ctx, value.ID)
	require.NoError(t, err)
	require.Equal(t, trip.StatusCompleted, completed.Status)
	require.NotNil(t, completed.FinishedAt)
	var histories int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM trip_status_history WHERE trip_id = $1", value.ID).Scan(&histories))
	require.Equal(t, 2, histories, "only the successful completion may add history")
}

func TestTransactionRollbackHistoryFailure(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	value := integrationTrip()
	cleanupTrips(t, pool, value.ID)

	err := tx.Do(ctx, func(txCtx context.Context) error {
		if err := repo.Create(txCtx, value); err != nil {
			return err
		}
		return repo.AddStatusHistory(txCtx, value.ID, nil, trip.Status("invalid"), "invalid transition")
	})
	var databaseErr *pgconn.PgError
	require.ErrorAs(t, err, &databaseErr)
	require.Equal(t, "23514", databaseErr.Code, "the second INSERT must fail a CHECK constraint")
	_, err = repo.GetByID(ctx, value.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound, "the first INSERT must be rolled back too")
	var histories int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM trip_status_history WHERE trip_id = $1", value.ID).Scan(&histories))
	require.Zero(t, histories)
}

func TestTransactionRollbackPanic(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	value := integrationTrip()
	cleanupTrips(t, pool, value.ID)
	var transactionErr error
	require.PanicsWithValue(t, "transaction interrupted", func() {
		transactionErr = tx.Do(ctx, func(txCtx context.Context) error {
			if err := repo.Create(txCtx, value); err != nil {
				return err
			}
			panic("transaction interrupted")
		})
	})
	require.NoError(t, transactionErr)
	_, err := repo.GetByID(ctx, value.ID)
	require.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestTransactionNestedUsesOuter(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	rollbackErr := errors.New("abort outer transaction")
	for _, abort := range []bool{false, true} {
		t.Run(fmt.Sprintf("abort=%t", abort), func(t *testing.T) {
			value := integrationTrip()
			cleanupTrips(t, pool, value.ID)
			var outerID, innerID int64
			err := tx.Do(ctx, func(outerCtx context.Context) error {
				if err := repo.Create(outerCtx, value); err != nil {
					return err
				}
				if err := transaction.Executor(outerCtx, pool).QueryRow(outerCtx, "SELECT txid_current()").Scan(&outerID); err != nil {
					return err
				}
				if err := tx.Do(outerCtx, func(innerCtx context.Context) error {
					if _, err := repo.GetByID(innerCtx, value.ID); err != nil {
						return err
					}
					if err := transaction.Executor(innerCtx, pool).QueryRow(innerCtx, "SELECT txid_current()").Scan(&innerID); err != nil {
						return err
					}
					return repo.AddStatusHistory(innerCtx, value.ID, nil, trip.StatusActive, "trip created")
				}); err != nil {
					return err
				}
				if abort {
					return rollbackErr
				}
				return nil
			})
			require.NotZero(t, outerID)
			require.Equal(t, outerID, innerID, "nested Do must reuse the same database transaction")
			if abort {
				require.ErrorIs(t, err, rollbackErr)
				_, err = repo.GetByID(ctx, value.ID)
				require.ErrorIs(t, err, trip.ErrTripNotFound, "nested Do must not commit independently")
				return
			}
			require.NoError(t, err)
			fetched, err := repo.GetByID(ctx, value.ID)
			require.NoError(t, err)
			requireSameTrip(t, value, fetched)
		})
	}
}

func integrationPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Fatal("integration tests require TEST_DATABASE_URL or DATABASE_URL; run tripgoctl environment start and connect, then make test-integration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err, "parse integration database configuration")
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(ctx), "connect to PostgreSQL; check tripgoctl environment and DATABASE_URL")
	return pool, ctx
}

func integrationTrip() trip.Trip {
	return trip.Trip{
		ID: uuid.New(), UserID: uuid.New(), DriverID: uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      100, Status: trip.StatusActive,
		StartedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func cleanupTrips(t *testing.T, pool *pgxpool.Pool, ids ...uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, id := range ids {
			if _, err := pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", id); err != nil {
				t.Errorf("delete integration test trip %s: %v", id, err)
			}
		}
	})
}

func createWithHistory(ctx context.Context, tx transaction.TxManager, repo *repository.TripRepository, value trip.Trip) error {
	return tx.Do(ctx, func(txCtx context.Context) error {
		if err := repo.Create(txCtx, value); err != nil {
			return err
		}
		return repo.AddStatusHistory(txCtx, value.ID, nil, trip.StatusActive, "trip created")
	})
}

func requireSameTrip(t *testing.T, want, actual trip.Trip) {
	t.Helper()
	require.Equal(t, want.ID, actual.ID)
	require.Equal(t, want.UserID, actual.UserID)
	require.Equal(t, want.DriverID, actual.DriverID)
	require.Equal(t, want.StartPoint, actual.StartPoint)
	require.Equal(t, want.EndPoint, actual.EndPoint)
	require.Equal(t, want.Price, actual.Price)
	require.Equal(t, want.Status, actual.Status)
	require.True(t, want.StartedAt.Equal(actual.StartedAt))
	if want.FinishedAt == nil {
		require.Nil(t, actual.FinishedAt)
	} else {
		require.NotNil(t, actual.FinishedAt)
		require.True(t, want.FinishedAt.Equal(*actual.FinishedAt))
	}
	require.Equal(t, want.LastPositionAt, actual.LastPositionAt)
}
