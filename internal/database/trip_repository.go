package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/npapaHAHA/tripgo/internal/trip"
)

const activeDriverConstraint = "trips_driver_active_uidx"

var tripColumns = []string{
	"id",
	"user_id",
	"driver_id",
	"start_latitude",
	"start_longitude",
	"end_latitude",
	"end_longitude",
	"price",
	"status",
	"started_at",
	"finished_at",
}

type TripRepository struct {
	pool    *pgxpool.Pool
	builder sq.StatementBuilderType
}

type rowScanner interface {
	Scan(dest ...any) error
}

func NewTripRepository(pool *pgxpool.Pool) *TripRepository {
	return &TripRepository{
		pool:    pool,
		builder: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

func (r *TripRepository) Create(ctx context.Context, value trip.Trip) error {
	query, args, err := r.builder.
		Insert("trips").
		Columns(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at",
		).
		Values(
			value.ID,
			value.UserID,
			value.DriverID,
			value.StartPoint.Latitude,
			value.StartPoint.Longitude,
			value.EndPoint.Latitude,
			value.EndPoint.Longitude,
			value.Price,
			value.Status,
			value.StartedAt,
			value.FinishedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build create trip query: %w", err)
	}

	if _, err := executorFromContext(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == activeDriverConstraint {
			return trip.ErrDriverBusy
		}

		return fmt.Errorf("create trip: %w", err)
	}

	return nil
}

func (r *TripRepository) AddStatusHistory(
	ctx context.Context,
	tripID uuid.UUID,
	fromStatus *trip.Status,
	toStatus trip.Status,
	reason string,
) error {
	var fromStatusValue any
	if fromStatus != nil {
		fromStatusValue = *fromStatus
	}

	query, args, err := r.builder.
		Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason").
		Values(tripID, fromStatusValue, toStatus, reason).
		ToSql()
	if err != nil {
		return fmt.Errorf("build add trip status history query: %w", err)
	}

	if _, err := executorFromContext(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("add trip status history: %w", err)
	}

	return nil
}

func (r *TripRepository) GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	query, args, err := r.builder.
		Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build get trip query: %w", err)
	}

	value, err := scanTrip(executorFromContext(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, trip.ErrNotFound
	}
	if err != nil {
		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return value, nil
}

func (r *TripRepository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (trip.Trip, error) {
	query, args, err := r.builder.
		Update("trips").
		Set("status", trip.StatusCompleted).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(sq.Eq{
			"id":     id,
			"status": trip.StatusActive,
		}).
		Suffix("RETURNING " + strings.Join(tripColumns, ", ")).
		ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build finish trip query: %w", err)
	}

	value, err := scanTrip(executorFromContext(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetByID(ctx, id); getErr != nil {
			return trip.Trip{}, getErr
		}

		return trip.Trip{}, trip.ErrCompleted
	}
	if err != nil {
		return trip.Trip{}, fmt.Errorf("finish trip: %w", err)
	}

	return value, nil
}

func scanTrip(row rowScanner) (trip.Trip, error) {
	var value trip.Trip
	var status string

	err := row.Scan(
		&value.ID,
		&value.UserID,
		&value.DriverID,
		&value.StartPoint.Latitude,
		&value.StartPoint.Longitude,
		&value.EndPoint.Latitude,
		&value.EndPoint.Longitude,
		&value.Price,
		&status,
		&value.StartedAt,
		&value.FinishedAt,
	)
	if err != nil {
		return trip.Trip{}, err
	}

	value.Status = trip.Status(status)

	return value, nil
}
