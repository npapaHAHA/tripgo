package create

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/model"
)

type UseCase struct {
	repository tripRepository
	tx         txManager
	timeout    time.Duration
	logger     *slog.Logger
	metrics    metrics
}

func New(repository tripRepository, tx txManager, timeout time.Duration, logger *slog.Logger, metrics metrics) *UseCase {
	return &UseCase{repository: repository, tx: tx, timeout: timeout, logger: logger, metrics: metrics}
}

func (u *UseCase) Execute(ctx context.Context, input model.CreateTripInput) (trip.Trip, error) {
	value, err := trip.New(input.UserID, input.DriverID, input.StartPoint, input.EndPoint, input.Price)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("validate trip: %w", err)
	}

	queryCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	err = u.tx.Do(queryCtx, func(txCtx context.Context) error {
		if err := u.repository.Create(txCtx, value); err != nil {
			return fmt.Errorf("save trip: %w", err)
		}

		if err := u.repository.AddStatusHistory(txCtx, value.ID, nil, trip.StatusActive, "trip created"); err != nil {
			return fmt.Errorf("save trip status history: %w", err)
		}

		return nil
	})
	if err != nil {
		if !errors.Is(err, trip.ErrDriverBusy) {
			u.logger.ErrorContext(ctx, "create trip failed", "error", err)
		}

		return trip.Trip{}, fmt.Errorf("create trip: %w", err)
	}

	u.metrics.TripCreated(ctx)
	u.logger.InfoContext(ctx, "trip created", "trip_id", value.ID.String())

	return value, nil
}
