package list

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

func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) ([]trip.Position, error) {
	queryCtx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	if _, err := u.repository.GetByID(queryCtx, id); err != nil {
		if !errors.Is(err, trip.ErrTripNotFound) {
			u.logger.ErrorContext(ctx, "get trip for positions failed", "error", err)
		}
		return nil, fmt.Errorf("get trip for positions: %w", err)
	}
	values, err := u.repository.ListPositions(queryCtx, id)
	if err != nil {
		u.logger.ErrorContext(ctx, "list positions failed", "error", err)
		return nil, fmt.Errorf("list positions: %w", err)
	}
	return values, nil
}
