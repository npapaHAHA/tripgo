//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

package finish

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

type tripRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	Finish(ctx context.Context, id uuid.UUID, at time.Time) (trip.Trip, error)
	AddStatusHistory(ctx context.Context, id uuid.UUID, from *trip.Status, to trip.Status, reason string) error
}

type txManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type metrics interface {
	TripCompleted(ctx context.Context)
}
