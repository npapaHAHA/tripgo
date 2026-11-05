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
	tests := []struct {
		name      string
		err       error
		cancelled bool
	}{
		{name: "success"},
		{name: "missing trip", err: trip.ErrTripNotFound},
		{name: "repository failure", err: storageErr},
		{name: "cancelled request", err: context.Canceled, cancelled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repository := NewMocktripRepository(ctrl)
			id := uuid.New()
			want := trip.Trip{ID: id, Status: trip.StatusActive, Price: 100}
			repository.EXPECT().GetByID(gomock.Any(), id).DoAndReturn(func(ctx context.Context, _ uuid.UUID) (trip.Trip, error) {
				_, hasDeadline := ctx.Deadline()
				require.True(t, hasDeadline)
				if tt.cancelled {
					require.ErrorIs(t, ctx.Err(), context.Canceled)
				}
				return want, tt.err
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			value, err := get.New(repository, time.Second, logger).Execute(ctx, id)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				require.Equal(t, trip.Trip{}, value)
				if errors.Is(tt.err, trip.ErrTripNotFound) {
					require.Empty(t, logs.String(), "a missing trip is an expected business error")
				} else {
					require.Equal(t, 1, strings.Count(logs.String(), `"level":"ERROR"`))
					require.Contains(t, logs.String(), tt.err.Error(), "the internal error must remain available in logs")
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, want, value)
			require.Empty(t, logs.String())
		})
	}
}
