//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

type tripRepository interface {
	UpdateLastPosition(ctx context.Context, id uuid.UUID, at time.Time) error
	AddPosition(ctx context.Context, value trip.Position) (trip.Position, error)
}

type txManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type pendingTasks interface {
	Cancel(ctx context.Context, id uuid.UUID)
}

type metrics interface {
	PositionSaved(ctx context.Context)
}
