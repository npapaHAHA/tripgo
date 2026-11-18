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
	id := uuid.New()
	active := trip.Trip{ID: id, Status: trip.StatusActive, Price: 100, StartedAt: time.Now().Add(-time.Hour)}
	completed := active
	completed.Status = trip.StatusCompleted
	finishedAt := active.StartedAt.Add(time.Hour)
	completed.FinishedAt = &finishedAt
	previous := trip.StatusActive
	storageErr := errors.New("database unavailable")
	runTransaction := func(ctx context.Context, fn func(context.Context) error) error {
		return fn(ctx)
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
				tx := NewMocktxManager(ctrl)
				metrics := NewMockmetrics(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil),
					repository.EXPECT().Finish(gomock.Any(), id, gomock.Cond(func(at time.Time) bool {
						return at.After(active.StartedAt)
					})).Return(completed, nil),
					repository.EXPECT().AddStatusHistory(gomock.Any(), id, &previous, trip.StatusCompleted, "trip completed").Return(nil),
				)
				metrics.EXPECT().TripCompleted(gomock.Any())
				return finish.New(repository, tx, time.Second, logger, metrics)
			},
			in:      id,
			want:    completed,
			wantLog: "trip completed",
		},
		{
			name: "missing trip",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
				return finish.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      id,
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "fetch failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, storageErr)
				return finish.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "already completed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(completed, nil)
				return finish.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      id,
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "concurrent completion",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil)
				repository.EXPECT().Finish(gomock.Any(), id, gomock.Any()).Return(trip.Trip{}, trip.ErrTripCompleted)
				return finish.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      id,
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "update failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil)
				repository.EXPECT().Finish(gomock.Any(), id, gomock.Any()).Return(trip.Trip{}, storageErr)
				return finish.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "history failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil)
				repository.EXPECT().Finish(gomock.Any(), id, gomock.Any()).Return(completed, nil)
				repository.EXPECT().AddStatusHistory(gomock.Any(), id, &previous, trip.StatusCompleted, "trip completed").Return(storageErr)
				return finish.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "commit failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *finish.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
					if err := fn(ctx); err != nil {
						return err
					}
					return storageErr
				})
				repository.EXPECT().GetByID(gomock.Any(), id).Return(active, nil)
				repository.EXPECT().Finish(gomock.Any(), id, gomock.Any()).Return(completed, nil)
				repository.EXPECT().AddStatusHistory(gomock.Any(), id, &previous, trip.StatusCompleted, "trip completed").Return(nil)
				return finish.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
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
				return finish.New(NewMocktripRepository(ctrl), tx, time.Second, logger, NewMockmetrics(ctrl))
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
			value, err := tt.setup(ctrl, logger).Execute(context.Background(), tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
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
