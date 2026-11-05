package finish

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
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

func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	queryCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	var result trip.Trip

	err := u.tx.Do(queryCtx, func(txCtx context.Context) error {
		value, err := u.repository.GetByID(txCtx, id)
		if err != nil {
			return fmt.Errorf("fetch trip: %w", err)
		}

		fromStatus := value.Status
		finishedAt := time.Now().UTC()
		if err := value.Finish(finishedAt); err != nil {
			return err
		}

		updated, err := u.repository.Finish(txCtx, id, finishedAt)
		if err != nil {
			return fmt.Errorf("save completed trip: %w", err)
		}
		if err := u.repository.AddStatusHistory(txCtx, id, &fromStatus, value.Status, "trip completed"); err != nil {
			return fmt.Errorf("save trip status history: %w", err)
		}

		result = updated

		return nil
	})
	if err != nil {
		if !errors.Is(err, trip.ErrTripNotFound) && !errors.Is(err, trip.ErrTripCompleted) {
			u.logger.ErrorContext(ctx, "finish trip failed", "error", err)
		}

		return trip.Trip{}, fmt.Errorf("finish trip: %w", err)
	}

	u.metrics.TripCompleted(ctx)
	u.logger.InfoContext(ctx, "trip completed", "trip_id", id.String())

	return result, nil
}
