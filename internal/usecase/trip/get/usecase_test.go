package get_test

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
	"github.com/npapaHAHA/tripgo/internal/usecase/trip/get"
)

func TestExecute(t *testing.T) {
	storageErr := errors.New("database unavailable")
	id := uuid.New()
	want := trip.Trip{ID: id, Status: trip.StatusActive, Price: 100}
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name    string
		setup   func(*gomock.Controller, *slog.Logger) *get.UseCase
		in      uuid.UUID
		ctx     context.Context
		want    trip.Trip
		wantErr error
		wantLog string
	}{
		{
			name: "success",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *get.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(want, nil)
				return get.New(repository, time.Second, logger)
			},
			in:   id,
			want: want,
		},
		{
			name: "missing trip",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *get.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
				return get.New(repository, time.Second, logger)
			},
			in:      id,
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "repository failure",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *get.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, storageErr)
				return get.New(repository, time.Second, logger)
			},
			in:      id,
			wantErr: storageErr,
			wantLog: storageErr.Error(),
		},
		{
			name: "cancelled request",
			setup: func(ctrl *gomock.Controller, logger *slog.Logger) *get.UseCase {
				repository := NewMocktripRepository(ctrl)
				repository.EXPECT().GetByID(gomock.Cond(func(ctx context.Context) bool {
					return ctx.Err() == context.Canceled
				}), id).Return(trip.Trip{}, context.Canceled)
				return get.New(repository, time.Second, logger)
			},
			in:      id,
			ctx:     cancelledCtx,
			wantErr: context.Canceled,
			wantLog: context.Canceled.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			value, err := tt.setup(ctrl, logger).Execute(ctx, tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want, value)
			if tt.wantLog == "" {
				require.Empty(t, logs.String())
			} else {
				require.Equal(t, 1, strings.Count(logs.String(), `"level":"ERROR"`))
				require.Contains(t, logs.String(), tt.wantLog)
			}
		})
	}
}
