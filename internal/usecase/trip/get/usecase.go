package get

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
	timeout    time.Duration
	logger     *slog.Logger
}

func New(repository tripRepository, timeout time.Duration, logger *slog.Logger) *UseCase {
	return &UseCase{repository: repository, timeout: timeout, logger: logger}
}

func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	queryCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()

	value, err := u.repository.GetByID(queryCtx, id)
	if err != nil {
		if !errors.Is(err, trip.ErrTripNotFound) {
			u.logger.ErrorContext(ctx, "get trip failed", "error", err)
		}

		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return value, nil
}
