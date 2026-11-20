package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/infra/postgres/transaction"
)

// UpdateLastPosition runs before AddPosition in the same transaction.
// Its row lock serializes saving a coordinate with finishing the trip.
func (r *TripRepository) UpdateLastPosition(ctx context.Context, id uuid.UUID, at time.Time) error {
	query, args, err := r.builder.Update("trips").
		Set("last_position_at", sq.Expr("GREATEST(last_position_at, ?)", at)).
		Set("updated_at", sq.Expr("CURRENT_TIMESTAMP")).
		Where(sq.Eq{"id": id, statusColumn: trip.StatusActive}).ToSql()
	if err != nil {
		return fmt.Errorf("build update last position query: %w", err)
	}
	result, err := transaction.Executor(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update last position: %w", err)
	}
	if result.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := transaction.Executor(ctx, r.pool).
		QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM trips WHERE id = $1)", id).Scan(&exists); err != nil {
		return fmt.Errorf("check trip exists: %w", err)
	}
	if !exists {
		return trip.ErrTripNotFound
	}
	return trip.ErrTripCompleted
}

func (r *TripRepository) AddPosition(ctx context.Context, value trip.Position) (trip.Position, error) {
	query, args, err := r.builder.Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(value.TripID, value.Coordinates.Latitude, value.Coordinates.Longitude, value.RecordedAt).
		Suffix("RETURNING id, recorded_at").ToSql()
	if err != nil {
		return trip.Position{}, fmt.Errorf("build add position query: %w", err)
	}
	if err := transaction.Executor(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&value.ID, &value.RecordedAt); err != nil {
		return trip.Position{}, fmt.Errorf("add position: %w", err)
	}
	value.RecordedAt = value.RecordedAt.UTC()
	return value, nil
}

func (r *TripRepository) ListPositions(ctx context.Context, id uuid.UUID) ([]trip.Position, error) {
	query, args, err := r.builder.Select("id", "trip_id", "latitude", "longitude", "recorded_at").
		From("trip_positions").Where(sq.Eq{"trip_id": id}).
		OrderBy("recorded_at ASC", "id ASC").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list positions query: %w", err)
	}
	rows, err := transaction.Executor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	defer rows.Close()

	values := make([]trip.Position, 0)
	for rows.Next() {
		var value trip.Position
		if err := rows.Scan(&value.ID, &value.TripID, &value.Coordinates.Latitude, &value.Coordinates.Longitude, &value.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		value.RecordedAt = value.RecordedAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read positions: %w", err)
	}
	return values, nil
}
