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
	"github.com/npapaHAHA/tripgo/internal/usecase/position/create"
)

func TestExecute(t *testing.T) {
	ctx := context.Background()
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	recordedAt := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return recordedAt.Add(time.Minute) }
	input := model.CreatePositionInput{
		TripID: id, Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335}, RecordedAt: recordedAt,
	}
	position := trip.Position{TripID: id, Coordinates: input.Coordinates, RecordedAt: recordedAt}
	saved := position
	saved.ID = 42
	storageErr := errors.New("database unavailable")
	tests := []struct {
		name    string
		setup   func(*gomock.Controller, *slog.Logger) *create.UseCase
		want    trip.Position
		wantErr error
		wantLog bool
	}{
		{
			name: "success",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				queue := NewMockpendingTasks(ctrl)
				metrics := NewMockmetrics(ctrl)
				gomock.InOrder(
					repository.EXPECT().UpdateLastPosition(gomock.Any(), id, recordedAt).Return(nil),
					repository.EXPECT().AddPosition(gomock.Any(), position).Return(saved, nil),
					queue.EXPECT().Cancel(ctx, id),
					metrics.EXPECT().PositionSaved(ctx),
				)
				return create.New(repository, txStub{}, time.Second, logger, metrics, queue, now)
			},
			want: saved,
		},
		{
			name: "trip not found",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().UpdateLastPosition(gomock.Any(), id, recordedAt).Return(trip.ErrTripNotFound)
				return create.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), NewMockpendingTasks(ctrl), now)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "trip completed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().UpdateLastPosition(gomock.Any(), id, recordedAt).Return(trip.ErrTripCompleted)
				return create.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), NewMockpendingTasks(ctrl), now)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "update failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().UpdateLastPosition(gomock.Any(), id, recordedAt).Return(storageErr)
				return create.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), NewMockpendingTasks(ctrl), now)
			},
			wantErr: storageErr,
			wantLog: true,
		},
		{
			name: "insert failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().UpdateLastPosition(gomock.Any(), id, recordedAt).Return(nil),
					repository.EXPECT().AddPosition(gomock.Any(), position).Return(trip.Position{}, storageErr),
				)
				return create.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), NewMockpendingTasks(ctrl), now)
			},
			wantErr: storageErr,
			wantLog: true,
		},
		{
			name: "commit failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().UpdateLastPosition(gomock.Any(), id, recordedAt).Return(nil),
					repository.EXPECT().AddPosition(gomock.Any(), position).Return(saved, nil),
				)
				return create.New(repository, txStub{commitErr: storageErr}, time.Second, logger, NewMockmetrics(ctrl), NewMockpendingTasks(ctrl), now)
			},
			wantErr: storageErr,
			wantLog: true,
		},
		{
			name: "begin failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(storageErr)
				return create.New(NewMocktripRepository(ctrl), tx, time.Second, logger, NewMockmetrics(ctrl), NewMockpendingTasks(ctrl), now)
			},
			wantErr: storageErr,
			wantLog: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			value, err := tt.setup(ctrl, logger).Execute(ctx, input)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, value)
			if tt.wantLog {
				require.Equal(t, 1, strings.Count(logs.String(), "\n"))
				require.Contains(t, logs.String(), storageErr.Error())
				require.Contains(t, logs.String(), `"level":"ERROR"`)
			} else {
				require.Empty(t, logs.String())
			}
		})
	}
}

func TestExecuteRejectsInvalidInput(t *testing.T) {
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input model.CreatePositionInput
		field string
	}{
		{name: "invalid coordinate", input: model.CreatePositionInput{Coordinates: trip.Coordinates{Latitude: 91}, RecordedAt: now}, field: "coordinates.latitude"},
		{name: "future timestamp", input: model.CreatePositionInput{RecordedAt: now.Add(2 * time.Minute)}, field: "recorded_at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			uc := create.New(NewMocktripRepository(ctrl), NewMocktxManager(ctrl), time.Second,
				slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), NewMockmetrics(ctrl), NewMockpendingTasks(ctrl), func() time.Time { return now })
			value, err := uc.Execute(context.Background(), tt.input)
			var validation *trip.ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, tt.field, validation.Field)
			require.Equal(t, trip.Position{}, value)
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
