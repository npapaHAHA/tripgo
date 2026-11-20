package list_test

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
	"github.com/npapaHAHA/tripgo/internal/usecase/position/list"
)

func TestExecute(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	recordedAt := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	positions := []trip.Position{{ID: 1, TripID: id, RecordedAt: recordedAt}, {ID: 2, TripID: id, RecordedAt: recordedAt}}
	storageErr := errors.New("database unavailable")
	tests := []struct {
		name    string
		setup   func(*MocktripRepository)
		want    []trip.Position
		wantErr error
		wantLog bool
	}{
		{
			name: "active trip route",
			setup: func(repository *MocktripRepository) {
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusActive}, nil),
					repository.EXPECT().ListPositions(gomock.Any(), id).Return(positions, nil),
				)
			},
			want: positions,
		},
		{
			name: "completed trip route",
			setup: func(repository *MocktripRepository) {
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusCompleted}, nil),
					repository.EXPECT().ListPositions(gomock.Any(), id).Return(positions, nil),
				)
			},
			want: positions,
		},
		{
			name: "empty route",
			setup: func(repository *MocktripRepository) {
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id}, nil),
					repository.EXPECT().ListPositions(gomock.Any(), id).Return([]trip.Position{}, nil),
				)
			},
			want: []trip.Position{},
		},
		{
			name: "missing trip",
			setup: func(repository *MocktripRepository) {
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "fetch failed",
			setup: func(repository *MocktripRepository) {
				repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, storageErr)
			},
			wantErr: storageErr,
			wantLog: true,
		},
		{
			name: "list failed",
			setup: func(repository *MocktripRepository) {
				gomock.InOrder(
					repository.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id}, nil),
					repository.EXPECT().ListPositions(gomock.Any(), id).Return(nil, storageErr),
				)
			},
			wantErr: storageErr,
			wantLog: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := NewMocktripRepository(gomock.NewController(t))
			tt.setup(repository)
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			values, err := list.New(repository, time.Second, logger).Execute(context.Background(), id)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, values)
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
