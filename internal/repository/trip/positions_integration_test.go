//go:build integration

package trip_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/infra/postgres/transaction"
	repository "github.com/npapaHAHA/tripgo/internal/repository/trip"
)

func TestRepositoryPositionsOutOfOrder(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	value := integrationTrip()
	cleanupTrips(t, pool, value.ID)
	require.NoError(t, repo.Create(ctx, value))
	positions, err := repo.ListPositions(ctx, value.ID)
	require.NoError(t, err)
	require.Empty(t, positions)
	require.NotNil(t, positions)

	newerAt := value.StartedAt.Add(time.Minute)
	newer, err := savePosition(ctx, tx, repo, trip.Position{
		TripID: value.ID, Coordinates: trip.Coordinates{Latitude: 55.75, Longitude: 37.61}, RecordedAt: newerAt,
	})
	require.NoError(t, err)
	older, err := savePosition(ctx, tx, repo, trip.Position{
		TripID: value.ID, Coordinates: trip.Coordinates{Latitude: 55.76, Longitude: 37.62}, RecordedAt: value.StartedAt,
	})
	require.NoError(t, err)
	tied, err := savePosition(ctx, tx, repo, trip.Position{
		TripID: value.ID, Coordinates: trip.Coordinates{Latitude: 55.77, Longitude: 37.63}, RecordedAt: newerAt,
	})
	require.NoError(t, err)
	require.Positive(t, newer.ID)
	require.Greater(t, tied.ID, newer.ID)

	positions, err = repo.ListPositions(ctx, value.ID)
	require.NoError(t, err)
	require.Equal(t, []trip.Position{older, newer, tied}, positions)
	fetched, err := repo.GetByID(ctx, value.ID)
	require.NoError(t, err)
	require.Equal(t, &newerAt, fetched.LastPositionAt)
	require.Equal(t, time.UTC, fetched.LastPositionAt.Location())

	completed, err := repo.Finish(ctx, value.ID, newerAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, &newerAt, completed.LastPositionAt)
}

func TestRepositoryUpdateLastPositionErrors(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	completed := integrationTrip()
	completed.Status = trip.StatusCompleted
	finishedAt := completed.StartedAt.Add(time.Minute)
	completed.FinishedAt = &finishedAt
	cleanupTrips(t, pool, completed.ID)
	require.NoError(t, repo.Create(ctx, completed))

	tests := []struct {
		name string
		id   uuid.UUID
		want error
	}{
		{name: "missing", id: uuid.New(), want: trip.ErrTripNotFound},
		{name: "completed", id: completed.ID, want: trip.ErrTripCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, repo.UpdateLastPosition(ctx, tt.id, finishedAt), tt.want)
		})
	}
	fetched, err := repo.GetByID(ctx, completed.ID)
	require.NoError(t, err)
	require.Nil(t, fetched.LastPositionAt)
}

func TestTransactionRollbackPosition(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	value := integrationTrip()
	cleanupTrips(t, pool, value.ID)
	require.NoError(t, repo.Create(ctx, value))
	initial, err := savePosition(ctx, tx, repo, trip.Position{
		TripID: value.ID, Coordinates: value.StartPoint, RecordedAt: value.StartedAt,
	})
	require.NoError(t, err)

	_, err = savePosition(ctx, tx, repo, trip.Position{
		TripID: value.ID, Coordinates: trip.Coordinates{Latitude: 91}, RecordedAt: value.StartedAt.Add(time.Minute),
	})
	var databaseErr *pgconn.PgError
	require.ErrorAs(t, err, &databaseErr)
	require.Equal(t, "23514", databaseErr.Code)
	fetched, err := repo.GetByID(ctx, value.ID)
	require.NoError(t, err)
	require.Equal(t, &value.StartedAt, fetched.LastPositionAt, "a failed INSERT must roll back the timestamp UPDATE")
	positions, err := repo.ListPositions(ctx, value.ID)
	require.NoError(t, err)
	require.Equal(t, []trip.Position{initial}, positions)
}

func TestRepositoryListStaleCriteria(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	values := make([]trip.Trip, 6)
	ids := make([]uuid.UUID, 0, len(values))
	for i := range values {
		values[i] = integrationTrip()
		ids = append(ids, values[i].ID)
	}
	cleanupTrips(t, pool, ids...)
	var found []trip.StaleTrip
	var wanted []trip.StaleTrip
	err := tx.Do(ctx, func(txCtx context.Context) error {
		var databaseNow time.Time
		if err := transaction.Executor(txCtx, pool).QueryRow(txCtx, "SELECT CURRENT_TIMESTAMP").Scan(&databaseNow); err != nil {
			return err
		}
		databaseNow = databaseNow.UTC()
		for i := range values {
			values[i].StartedAt = databaseNow.Add(-10 * time.Minute)
		}
		values[0].StartedAt = databaseNow.Add(-2 * time.Minute)
		stalePositionAt := databaseNow.Add(-90 * time.Second)
		values[1].LastPositionAt = &stalePositionAt
		values[2].StartedAt = databaseNow.Add(-30 * time.Second)
		freshPositionAt := databaseNow.Add(-30 * time.Second)
		values[3].LastPositionAt = &freshPositionAt
		values[4].Status = trip.StatusCompleted
		finishedAt := databaseNow.Add(-5 * time.Minute)
		values[4].FinishedAt = &finishedAt
		values[5].StartedAt = databaseNow.Add(-time.Minute)
		for _, value := range values {
			if err := repo.Create(txCtx, value); err != nil {
				return err
			}
		}
		wanted = []trip.StaleTrip{
			{ID: values[0].ID, DriverID: values[0].DriverID, LastSeenAt: values[0].StartedAt},
			{ID: values[1].ID, DriverID: values[1].DriverID, LastSeenAt: stalePositionAt},
		}
		result, err := readStalePages(txCtx, repo, time.Minute, nil, 100)
		found = ownStaleTrips(result, ids)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, wanted, found, "only stale active trips belong in the result, oldest first; equality at cutoff is fresh")
}

func TestRepositoryListStaleKeysetEqualTimestamps(t *testing.T) {
	pool, ctx := integrationPool(t)
	repo := repository.NewTripRepository(pool)
	tx := transaction.New(pool)
	values := make([]trip.Trip, 5)
	ids := make([]uuid.UUID, 0, len(values))
	for i := range values {
		values[i] = integrationTrip()
		ids = append(ids, values[i].ID)
	}
	cleanupTrips(t, pool, ids...)
	sort.Slice(values, func(i, j int) bool { return values[i].ID.String() < values[j].ID.String() })
	var found []trip.StaleTrip
	wanted := make([]trip.StaleTrip, 0, len(values))
	err := tx.Do(ctx, func(txCtx context.Context) error {
		var databaseNow time.Time
		if err := transaction.Executor(txCtx, pool).QueryRow(txCtx, "SELECT CURRENT_TIMESTAMP").Scan(&databaseNow); err != nil {
			return err
		}
		lastSeenAt := databaseNow.UTC().Add(-2 * time.Minute)
		for _, value := range values {
			value.StartedAt = lastSeenAt
			if err := repo.Create(txCtx, value); err != nil {
				return err
			}
			wanted = append(wanted, trip.StaleTrip{ID: value.ID, DriverID: value.DriverID, LastSeenAt: lastSeenAt})
		}
		start := &trip.StaleTrip{LastSeenAt: lastSeenAt}
		result, err := readStalePages(txCtx, repo, time.Minute, start, 2)
		found = ownStaleTrips(result, ids)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, wanted, found, "keyset pages must neither skip nor repeat trips at the same timestamp")
}

func savePosition(ctx context.Context, tx transaction.TxManager, repo *repository.TripRepository, value trip.Position) (trip.Position, error) {
	var saved trip.Position
	err := tx.Do(ctx, func(txCtx context.Context) error {
		if err := repo.UpdateLastPosition(txCtx, value.TripID, value.RecordedAt); err != nil {
			return err
		}
		position, err := repo.AddPosition(txCtx, value)
		saved = position
		return err
	})
	if err != nil {
		return trip.Position{}, err
	}
	return saved, nil
}

func readStalePages(ctx context.Context, repo *repository.TripRepository, staleAfter time.Duration, after *trip.StaleTrip, limit int) ([]trip.StaleTrip, error) {
	values := make([]trip.StaleTrip, 0)
	for {
		page, err := repo.ListStale(ctx, staleAfter, after, limit)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return values, nil
		}
		last := page[len(page)-1]
		if after != nil && (last.LastSeenAt.Before(after.LastSeenAt) ||
			(last.LastSeenAt.Equal(after.LastSeenAt) && last.ID.String() <= after.ID.String())) {
			return nil, fmt.Errorf("stale trip cursor did not advance: %s", last.ID)
		}
		values = append(values, page...)
		after = &last
	}
}

func ownStaleTrips(values []trip.StaleTrip, ids []uuid.UUID) []trip.StaleTrip {
	owned := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		owned[id] = struct{}{}
	}
	filtered := make([]trip.StaleTrip, 0, len(ids))
	for _, value := range values {
		if _, ok := owned[value.ID]; ok {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
