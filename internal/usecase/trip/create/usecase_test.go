package create_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	input := model.CreateTripInput{
		UserID: uuid.New(), DriverID: uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      100,
	}
	invalidInput := input
	invalidInput.Price = -1
	storageErr := errors.New("database unavailable")
	var saved trip.Trip
	runTransaction := func(ctx context.Context, fn func(context.Context) error) error {
		return fn(ctx)
	}

	tests := []struct {
		name    string
		setup   func(*gomock.Controller, *slog.Logger) *create.UseCase
		in      model.CreateTripInput
		want    func(*testing.T, trip.Trip)
		wantErr func(*testing.T, error)
	}{
		{
			name: "success",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				metrics := NewMockmetrics(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				gomock.InOrder(
					repository.EXPECT().Create(gomock.Any(), gomock.Cond(func(value trip.Trip) bool {
						return value.UserID == input.UserID && value.DriverID == input.DriverID &&
							value.StartPoint == input.StartPoint && value.EndPoint == input.EndPoint && value.Price == input.Price
					})).DoAndReturn(func(_ context.Context, value trip.Trip) error {
						saved = value
						return nil
					}),
					repository.EXPECT().AddStatusHistory(gomock.Any(), gomock.Cond(func(id uuid.UUID) bool {
						return id == saved.ID
					}), nil, trip.StatusActive, "trip created").Return(nil),
				)
				metrics.EXPECT().TripCreated(gomock.Any())
				return create.New(repository, tx, time.Second, logger, metrics)
			},
			in: input,
			want: func(t *testing.T, value trip.Trip) {
				require.Equal(t, saved, value)
			},
		},
		{
			name: "invalid input",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				return create.New(NewMocktripRepository(ctrl), NewMocktxManager(ctrl), time.Second, logger, NewMockmetrics(ctrl))
			},
			in: invalidInput,
			wantErr: func(t *testing.T, err error) {
				var validation *trip.ValidationError
				require.ErrorAs(t, err, &validation)
				require.Equal(t, "price", validation.Field)
			},
		},
		{
			name: "driver busy",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().Create(gomock.Any(), gomock.Any()).Return(trip.ErrDriverBusy)
				return create.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      input,
			wantErr: func(t *testing.T, err error) { require.ErrorIs(t, err, trip.ErrDriverBusy) },
		},
		{
			name: "insert failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().Create(gomock.Any(), gomock.Any()).Return(storageErr)
				return create.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      input,
			wantErr: func(t *testing.T, err error) { require.ErrorIs(t, err, storageErr) },
		},
		{
			name: "history failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(runTransaction)
				repository.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				repository.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), nil, trip.StatusActive, "trip created").Return(storageErr)
				return create.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      input,
			wantErr: func(t *testing.T, err error) { require.ErrorIs(t, err, storageErr) },
		},
		{
			name: "commit failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
					if err := fn(ctx); err != nil {
						return err
					}
					return storageErr
				})
				repository.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				repository.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), nil, trip.StatusActive, "trip created").Return(nil)
				return create.New(repository, tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      input,
			wantErr: func(t *testing.T, err error) { require.ErrorIs(t, err, storageErr) },
		},
		{
			name: "begin failed",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *create.UseCase {
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(storageErr)
				return create.New(NewMocktripRepository(ctrl), tx, time.Second, logger, NewMockmetrics(ctrl))
			},
			in:      input,
			wantErr: func(t *testing.T, err error) { require.ErrorIs(t, err, storageErr) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			value, err := tt.setup(ctrl, logger).Execute(context.Background(), tt.in)
			if tt.wantErr != nil {
				tt.wantErr(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.want != nil {
				tt.want(t, value)
			} else {
				require.Equal(t, trip.Trip{}, value)
			}
		})
	}
}
