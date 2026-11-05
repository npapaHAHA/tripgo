//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test

package create

import (
	"context"

	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

type tripRepository interface {
	Create(ctx context.Context, value trip.Trip) error
	AddStatusHistory(ctx context.Context, id uuid.UUID, from *trip.Status, to trip.Status, reason string) error
}

type txManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type metrics interface {
	TripCreated(ctx context.Context)
}
