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
	queue      pendingTasks
	now        func() time.Time
}

func New(repository tripRepository, tx txManager, timeout time.Duration, logger *slog.Logger, metrics metrics, queue pendingTasks, now func() time.Time) *UseCase {
	return &UseCase{repository: repository, tx: tx, timeout: timeout, logger: logger, metrics: metrics, queue: queue, now: now}
}

func (u *UseCase) Execute(ctx context.Context, input model.CreatePositionInput) (trip.Position, error) {
	if err := input.Validate(u.now()); err != nil {
		return trip.Position{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	value := trip.Position{TripID: input.TripID, Coordinates: input.Coordinates, RecordedAt: input.RecordedAt.UTC()}
	var saved trip.Position
	err := u.tx.Do(queryCtx, func(txCtx context.Context) error {
		if err := u.repository.UpdateLastPosition(txCtx, value.TripID, value.RecordedAt); err != nil {
			return fmt.Errorf("update last position: %w", err)
		}
		var err error
		saved, err = u.repository.AddPosition(txCtx, value)
		if err != nil {
			return fmt.Errorf("insert position: %w", err)
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, trip.ErrTripNotFound) && !errors.Is(err, trip.ErrTripCompleted) {
			u.logger.ErrorContext(ctx, "save position failed", "error", err)
		}
		return trip.Position{}, fmt.Errorf("save position: %w", err)
	}
	u.queue.Cancel(ctx, value.TripID)
	u.metrics.PositionSaved(ctx)
	return saved, nil
}
