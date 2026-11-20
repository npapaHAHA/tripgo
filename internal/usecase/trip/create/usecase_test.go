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
	id := uuid.MustParse("8ed1b6e0-2483-452c-a241-f6acfe4a1b68")
	startedAt := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	newID := func() uuid.UUID { return id }
	now := func() time.Time { return startedAt }
	input := model.CreateTripInput{
		UserID:     uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID:   uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      100,
	}
	expected := trip.Trip{
		ID: id, UserID: input.UserID, DriverID: input.DriverID,
		StartPoint: input.StartPoint, EndPoint: input.EndPoint, Price: input.Price,
		Status: trip.StatusActive, StartedAt: startedAt,
	}
	invalidInput := input
	invalidInput.Price = -1
	storageErr := errors.New("database unavailable")
	wantStorageError := func(t *testing.T, err error) {
		t.Helper()
		require.ErrorIs(t, err, storageErr)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name    string
		setup   func(*gomock.Controller) *create.UseCase
		in      model.CreateTripInput
		want    trip.Trip
		wantErr func(*testing.T, error)
	}{
		{
			name: "success",
			setup: func(ctrl *gomock.Controller) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				metrics := NewMockmetrics(ctrl)
				gomock.InOrder(
					repository.EXPECT().Create(gomock.Any(), expected).Return(nil),
					repository.EXPECT().AddStatusHistory(gomock.Any(), id, nil, trip.StatusActive, "trip created").Return(nil),
					metrics.EXPECT().TripCreated(context.Background()).Return(),
				)
				return create.New(repository, txStub{}, time.Second, logger, metrics, newID, now)
			},
			in:   input,
			want: expected,
		},
		{
			name: "invalid input",
			setup: func(ctrl *gomock.Controller) *create.UseCase {
				return create.New(NewMocktripRepository(ctrl), NewMocktxManager(ctrl), time.Second, logger, NewMockmetrics(ctrl), newID, now)
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
			setup: func(ctrl *gomock.Controller) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().Create(gomock.Any(), expected).Return(trip.ErrDriverBusy)
				return create.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), newID, now)
			},
			in:      input,
			wantErr: func(t *testing.T, err error) { require.ErrorIs(t, err, trip.ErrDriverBusy) },
		},
		{
			name: "insert failed",
			setup: func(ctrl *gomock.Controller) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().Create(gomock.Any(), expected).Return(storageErr)
				return create.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), newID, now)
			},
			in:      input,
			wantErr: wantStorageError,
		},
		{
			name: "history failed",
			setup: func(ctrl *gomock.Controller) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().Create(gomock.Any(), expected).Return(nil),
					repository.EXPECT().AddStatusHistory(gomock.Any(), id, nil, trip.StatusActive, "trip created").Return(storageErr),
				)
				return create.New(repository, txStub{}, time.Second, logger, NewMockmetrics(ctrl), newID, now)
			},
			in:      input,
			wantErr: wantStorageError,
		},
		{
			name: "commit failed",
			setup: func(ctrl *gomock.Controller) *create.UseCase {
				repository := NewMocktripRepository(ctrl)
				gomock.InOrder(
					repository.EXPECT().Create(gomock.Any(), expected).Return(nil),
					repository.EXPECT().AddStatusHistory(gomock.Any(), id, nil, trip.StatusActive, "trip created").Return(nil),
				)
				return create.New(repository, txStub{commitErr: storageErr}, time.Second, logger, NewMockmetrics(ctrl), newID, now)
			},
			in:      input,
			wantErr: wantStorageError,
		},
		{
			name: "begin failed",
			setup: func(ctrl *gomock.Controller) *create.UseCase {
				tx := NewMocktxManager(ctrl)
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(storageErr)
				return create.New(NewMocktripRepository(ctrl), tx, time.Second, logger, NewMockmetrics(ctrl), newID, now)
			},
			in:      input,
			wantErr: wantStorageError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			value, err := tt.setup(ctrl).Execute(context.Background(), tt.in)
			if tt.wantErr != nil {
				tt.wantErr(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want, value)
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
