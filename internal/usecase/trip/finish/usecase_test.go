package finish_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/usecase/trip/finish"
)

func TestExecute(t *testing.T) {
	ctx := context.Background()
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	active := trip.Trip{
		ID:        id,
		Status:    trip.StatusActive,
		Price:     100,
		StartedAt: time.Date(2026, time.November, 20, 9, 0, 0, 0, time.UTC),
	}
	completed := active
	completed.Status = trip.StatusCompleted
	finishedAt := active.StartedAt.Add(time.Hour)
	completed.FinishedAt = &finishedAt
	previous := trip.StatusActive
	storageErr := errors.New("database unavailable")
	now := func() time.Time {
		return finishedAt
	}

	tests := []struct {
		name    string
		setup   func(*gomock.Controller, *slog.Logger) *finish.UseCase
		in      uuid.UUID
		want    trip.Trip
		wantErr error
		wantLog string
	}{
		{
			name: "success",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				metrics := NewMockmetrics(ctrl)
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil),
					repository.EXPECT().Finish(gomock.Any(), id, finishedAt).Return(completed, nil),
					repository.EXPECT().AddStatusHistory(gomock.Any(), id, &previous, trip.StatusCompleted, "trip completed").Return(nil),
					metrics.EXPECT().TripCompleted(ctx),
				)
				return finish.New(repository, txStub{}, time.Second, logger, metrics, now)
			},
			in:      id,
			want:    completed,
			wantLog: "trip completed",
		},
		{
			name: "missing trip",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
				return finish.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "fetch failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, storageErr)
				return finish.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "already completed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(completed, nil)
				return finish.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "concurrent completion",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil),
					repository.EXPECT().Finish(gomock.Any(), id, finishedAt).Return(trip.Trip{}, trip.ErrTripCompleted),
				)
				return finish.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "update failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil),
					repository.EXPECT().Finish(gomock.Any(), id, finishedAt).Return(trip.Trip{}, storageErr),
				)
				return finish.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "history failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil),
					repository.EXPECT().Finish(gomock.Any(), id, finishedAt).Return(completed, nil),
					repository.EXPECT().AddStatusHistory(gomock.Any(), id, &previous, trip.StatusCompleted, "trip completed").Return(storageErr),
				)
				return finish.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "commit failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil),
					repository.EXPECT().Finish(gomock.Any(), id, finishedAt).Return(completed, nil),
					repository.EXPECT().AddStatusHistory(gomock.Any(), id, &previous, trip.StatusCompleted, "trip completed").Return(nil),
				)
				return finish.New(repository, txStub{commitErr: storageErr}, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "begin failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(storageErr)
				return finish.New(NewMocktripRepository(ctrl), tx, time.Second, logger, NewMockmetrics(ctrl), now)
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			value, err := tt.setup(ctrl, logger).Execute(ctx, tt.in)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, value)
			if tt.wantLog == "" {
				require.Empty(t, logs.String())
			} else {
				require.Equal(t, 1, strings.Count(logs.String(), "\n"))
				require.Contains(t, logs.String(), tt.wantLog)
				if tt.wantErr != nil {
					require.Contains(t, logs.String(), `"level":"ERROR"`)
				}
			}
		})
	}
}

type txStub struct {
	commitErr error
}

func (s txStub) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := fn(ctx); err != nil {
		return err
	}
	return s.commitErr
}
