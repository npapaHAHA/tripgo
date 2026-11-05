package create_test

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
	"github.com/npapaHAHA/tripgo/internal/model"
	"github.com/npapaHAHA/tripgo/internal/usecase/trip/create"
)

func TestExecute(t *testing.T) {
	ctrl := gomock.NewController(t)
	repository := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockmetrics(ctrl)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	input := validInput()
	var saved trip.Trip
	committed := false
	type transactionKey struct{}

	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
		_, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline, "database work must have a timeout")
		err := fn(context.WithValue(ctx, transactionKey{}, true))
		committed = err == nil
		return err
	})
	gomock.InOrder(
		repository.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, value trip.Trip) error {
			require.Equal(t, true, ctx.Value(transactionKey{}), "repository must receive the transaction context")
			saved = value
			require.NotEqual(t, uuid.Nil, value.ID)
			require.Equal(t, input.UserID, value.UserID)
			require.Equal(t, input.DriverID, value.DriverID)
			require.Equal(t, input.StartPoint, value.StartPoint)
			require.Equal(t, input.EndPoint, value.EndPoint)
			require.Equal(t, input.Price, value.Price)
			require.Equal(t, trip.StatusActive, value.Status)
			require.False(t, value.StartedAt.IsZero())
			require.Nil(t, value.FinishedAt)
			return nil
		}),
		repository.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), nil, trip.StatusActive, "trip created").
			DoAndReturn(func(ctx context.Context, id uuid.UUID, _ *trip.Status, _ trip.Status, _ string) error {
				require.Equal(t, true, ctx.Value(transactionKey{}), "history must use the same transaction")
				require.Equal(t, saved.ID, id)
				return nil
			}),
	)
	metrics.EXPECT().TripCreated(gomock.Any()).Do(func(_ context.Context) {
		require.True(t, committed, "a trip is counted only after commit")
	})

	value, err := create.New(repository, tx, time.Second, logger, metrics).Execute(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, saved, value)
	require.Contains(t, logs.String(), `"msg":"trip created"`)
	require.Contains(t, logs.String(), value.ID.String())
}

func TestExecuteRejectsInvalidInputBeforeTransaction(t *testing.T) {
	ctrl := gomock.NewController(t)
	input := validInput()
	input.Price = -1
	usecase := create.New(NewMocktripRepository(ctrl), NewMocktxManager(ctrl), time.Second,
		slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), NewMockmetrics(ctrl))

	value, err := usecase.Execute(context.Background(), input)
	var validation *trip.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "price", validation.Field)
	require.Equal(t, trip.Trip{}, value)
}

func TestExecuteFailure(t *testing.T) {
	storageErr := errors.New("database unavailable")
	tests := []struct {
		name       string
		createErr  error
		historyErr error
		txErr      error
		beginFails bool
		wantErr    error
		wantLogs   int
	}{
		{name: "busy driver", createErr: trip.ErrDriverBusy, wantErr: trip.ErrDriverBusy},
		{name: "insert failed", createErr: storageErr, wantErr: storageErr, wantLogs: 1},
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
				repository.EXPECT().Create(gomock.Any(), gomock.Any()).Return(tt.createErr)
				if tt.createErr == nil {
					repository.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), nil, trip.StatusActive, "trip created").Return(tt.historyErr)
				}
			}

			value, err := create.New(repository, tx, time.Second,
				slog.New(slog.NewJSONHandler(&logs, nil)), metrics).Execute(context.Background(), validInput())
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, trip.Trip{}, value)
			require.Equal(t, tt.wantLogs, strings.Count(logs.String(), `"level":"ERROR"`))
			require.NotContains(t, logs.String(), `"msg":"trip created"`)
		})
	}
}

func validInput() model.CreateTripInput {
	return model.CreateTripInput{
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      100,
	}
}
