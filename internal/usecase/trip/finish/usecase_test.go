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
	ctrl := gomock.NewController(t)
	repository := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockmetrics(ctrl)
	id := uuid.New()
	initial := trip.Trip{ID: id, Status: trip.StatusActive, Price: 100, StartedAt: time.Now().Add(-time.Hour)}
	var completed trip.Trip
	committed := false
	type transactionKey struct{}
	var logs bytes.Buffer
	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
		_, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		err := fn(context.WithValue(ctx, transactionKey{}, true))
		committed = err == nil
		return err
	})
	gomock.InOrder(
		repository.EXPECT().GetByID(gomock.Any(), id).DoAndReturn(func(ctx context.Context, _ uuid.UUID) (trip.Trip, error) {
			require.Equal(t, true, ctx.Value(transactionKey{}))
			return initial, nil
		}),
		repository.EXPECT().Finish(gomock.Any(), id, gomock.Any()).DoAndReturn(func(ctx context.Context, _ uuid.UUID, at time.Time) (trip.Trip, error) {
			require.Equal(t, true, ctx.Value(transactionKey{}))
			require.True(t, at.After(initial.StartedAt))
			completed = initial
			completed.Status = trip.StatusCompleted
			completed.FinishedAt = &at
			return completed, nil
		}),
		repository.EXPECT().AddStatusHistory(gomock.Any(), id, gomock.Any(), trip.StatusCompleted, "trip completed").
			DoAndReturn(func(ctx context.Context, _ uuid.UUID, from *trip.Status, _ trip.Status, _ string) error {
				require.Equal(t, true, ctx.Value(transactionKey{}), "history must use the same transaction")
				require.NotNil(t, from)
				require.Equal(t, trip.StatusActive, *from)
				return nil
			}),
	)
	metrics.EXPECT().TripCompleted(gomock.Any()).Do(func(_ context.Context) {
		require.True(t, committed, "completion is counted only after commit")
	})

	value, err := finish.New(repository, tx, time.Second,
		slog.New(slog.NewJSONHandler(&logs, nil)), metrics).Execute(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, completed, value)
	require.Contains(t, logs.String(), `"msg":"trip completed"`)
	require.Contains(t, logs.String(), id.String())
}

func TestExecuteFailure(t *testing.T) {
	storageErr := errors.New("database unavailable")
	tests := []struct {
		name       string
		status     trip.Status
		fetchErr   error
		finishErr  error
		historyErr error
		txErr      error
		beginFails bool
		wantErr    error
		wantLogs   int
	}{
		{name: "missing trip", fetchErr: trip.ErrTripNotFound, wantErr: trip.ErrTripNotFound},
		{name: "fetch failed", fetchErr: storageErr, wantErr: storageErr, wantLogs: 1},
		{name: "already completed", status: trip.StatusCompleted, wantErr: trip.ErrTripCompleted},
		{name: "concurrent completion", finishErr: trip.ErrTripCompleted, wantErr: trip.ErrTripCompleted},
		{name: "update failed", finishErr: storageErr, wantErr: storageErr, wantLogs: 1},
		{name: "history failed", historyErr: storageErr, wantErr: storageErr, wantLogs: 1},
		{name: "commit failed", txErr: storageErr, wantErr: storageErr, wantLogs: 1},
		{name: "begin failed", txErr: storageErr, beginFails: true, wantErr: storageErr, wantLogs: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repository := NewMocktripRepository(ctrl)
			tx := NewMocktxManager(ctrl)
			metrics := NewMockmetrics(ctrl)
			id := uuid.New()
			status := tt.status
			if status == "" {
				status = trip.StatusActive
			}
			var logs bytes.Buffer
			tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
				if tt.beginFails {
					return tt.txErr
				}
				if err := fn(ctx); err != nil {
					return err
				}
				return tt.txErr
			})
			if !tt.beginFails {
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id, Status: status}, tt.fetchErr)
				if tt.fetchErr == nil && status == trip.StatusActive {
					repository.EXPECT().Finish(gomock.Any(), id, gomock.Any()).Return(trip.Trip{ID: id, Status: trip.StatusCompleted}, tt.finishErr)
					if tt.finishErr == nil {
						repository.EXPECT().AddStatusHistory(gomock.Any(), id, gomock.Any(), trip.StatusCompleted, "trip completed").Return(tt.historyErr)
					}
				}
			}

			value, err := finish.New(repository, tx, time.Second,
				slog.New(slog.NewJSONHandler(&logs, nil)), metrics).Execute(context.Background(), id)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, trip.Trip{}, value)
			require.Equal(t, tt.wantLogs, strings.Count(logs.String(), `"level":"ERROR"`))
			require.NotContains(t, logs.String(), `"msg":"trip completed"`)
		})
	}
}
