package trip_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

func TestNew(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	userID, driverID := uuid.New(), uuid.New()
	start := trip.Coordinates{Latitude: 59.9398, Longitude: 30.3146}
	end := trip.Coordinates{Latitude: 59.9290, Longitude: 30.3626}
	startedAt := time.Date(2026, time.November, 5, 15, 0, 0, 123456789, time.FixedZone("UTC+3", 3*60*60))
	got, err := trip.New(id, userID, driverID, start, end, 1450, startedAt)

	require.NoError(t, err)
	require.Equal(t, trip.Trip{
		ID:         id,
		UserID:     userID,
		DriverID:   driverID,
		StartPoint: start,
		EndPoint:   end,
		Price:      1450,
		Status:     trip.StatusActive,
		StartedAt:  time.Date(2026, time.November, 5, 12, 0, 0, 123456000, time.UTC),
	}, got)
}

func TestNewRejectsInvalidInput(t *testing.T) {
	value, err := trip.New(uuid.New(), uuid.New(), uuid.New(), trip.Coordinates{}, trip.Coordinates{}, -1, time.Time{})
	var validation *trip.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, trip.Trip{}, value)
}

func TestFinish(t *testing.T) {
	startedAt := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Hour)
	earlierFinish := finishedAt.Add(-time.Minute)
	active := trip.Trip{Status: trip.StatusActive, StartedAt: startedAt}
	completed := trip.Trip{Status: trip.StatusCompleted, StartedAt: startedAt, FinishedAt: &earlierFinish}
	tests := []struct {
		name    string
		in      trip.Trip
		want    trip.Trip
		wantErr error
	}{
		{
			name: "finish active trip",
			in:   active,
			want: trip.Trip{Status: trip.StatusCompleted, StartedAt: startedAt, FinishedAt: &finishedAt},
		},
		{
			name:    "keep the original completion time on repeat finish",
			in:      completed,
			want:    completed,
			wantErr: trip.ErrTripCompleted,
		},
		{
			name:    "reject unknown status",
			wantErr: trip.ErrTripCompleted,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			err := got.Finish(finishedAt)

			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidate(t *testing.T) {
	const startLatitudeField = "start_point.latitude"

	tests := []struct {
		name   string
		change func(*trip.Trip)
		field  string
	}{
		{name: "valid", change: func(_ *trip.Trip) {}},
		{name: "empty user", change: func(v *trip.Trip) { v.UserID = uuid.Nil }, field: "user_id"},
		{name: "empty driver", change: func(v *trip.Trip) { v.DriverID = uuid.Nil }, field: "driver_id"},
		{name: "negative price", change: func(v *trip.Trip) { v.Price = -1 }, field: "price"},
		{name: "start latitude low", change: func(v *trip.Trip) { v.StartPoint.Latitude = -91 }, field: startLatitudeField},
		{name: "start latitude high", change: func(v *trip.Trip) { v.StartPoint.Latitude = 91 }, field: startLatitudeField},
		{name: "start longitude low", change: func(v *trip.Trip) { v.StartPoint.Longitude = -181 }, field: "start_point.longitude"},
		{name: "start longitude high", change: func(v *trip.Trip) { v.StartPoint.Longitude = 181 }, field: "start_point.longitude"},
		{name: "end latitude", change: func(v *trip.Trip) { v.EndPoint.Latitude = 91 }, field: "end_point.latitude"},
		{name: "end longitude", change: func(v *trip.Trip) { v.EndPoint.Longitude = 181 }, field: "end_point.longitude"},
		{name: "NaN latitude", change: func(v *trip.Trip) { v.StartPoint.Latitude = math.NaN() }, field: startLatitudeField},
		{name: "NaN longitude", change: func(v *trip.Trip) { v.EndPoint.Longitude = math.NaN() }, field: "end_point.longitude"},
		{name: "infinite coordinate", change: func(v *trip.Trip) { v.EndPoint.Latitude = math.Inf(1) }, field: "end_point.latitude"},
		{
			name: "boundary coordinates and zero price",
			change: func(v *trip.Trip) {
				v.StartPoint = trip.Coordinates{Latitude: -90, Longitude: -180}
				v.EndPoint = trip.Coordinates{Latitude: 90, Longitude: 180}
				v.Price = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := trip.Trip{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}
			tt.change(&value)

			err := value.Validate()
			if tt.field == "" {
				require.NoError(t, err)
				return
			}

			var validation *trip.ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, tt.field, validation.Field)
			require.NotEmpty(t, validation.Message)
			require.Contains(t, err.Error(), tt.field)
		})
	}
}

func TestValidationErrorWithoutField(t *testing.T) {
	err := &trip.ValidationError{Message: "invalid request body"}
	require.EqualError(t, err, "invalid request body")
}
