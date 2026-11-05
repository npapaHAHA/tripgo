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
	userID, driverID := uuid.New(), uuid.New()
	start := trip.Coordinates{Latitude: 59.9398, Longitude: 30.3146}
	end := trip.Coordinates{Latitude: 59.9290, Longitude: 30.3626}
	before := time.Now().UTC().Truncate(time.Microsecond)

	value, err := trip.New(userID, driverID, start, end, 1450)
	after := time.Now().UTC()

	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, value.ID)
	require.Equal(t, userID, value.UserID)
	require.Equal(t, driverID, value.DriverID)
	require.Equal(t, start, value.StartPoint)
	require.Equal(t, end, value.EndPoint)
	require.Equal(t, int64(1450), value.Price)
	require.Equal(t, trip.StatusActive, value.Status)
	require.Nil(t, value.FinishedAt)
	require.Nil(t, value.LastPositionAt)
	require.Equal(t, time.UTC, value.StartedAt.Location())
	require.False(t, value.StartedAt.Before(before))
	require.False(t, value.StartedAt.After(after))
	require.Zero(t, value.StartedAt.Nanosecond()%int(time.Microsecond))
}

func TestNewRejectsInvalidInput(t *testing.T) {
	value, err := trip.New(uuid.New(), uuid.New(), trip.Coordinates{}, trip.Coordinates{}, -1)
	var validation *trip.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, trip.Trip{}, value)
}

func TestFinish(t *testing.T) {
	startedAt := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Hour)
	value := trip.Trip{ID: uuid.New(), Status: trip.StatusActive, StartedAt: startedAt}

	require.NoError(t, value.Finish(finishedAt))
	require.Equal(t, trip.StatusCompleted, value.Status)
	require.Equal(t, &finishedAt, value.FinishedAt)
	require.Equal(t, startedAt, value.StartedAt)

	completed := value
	require.ErrorIs(t, value.Finish(finishedAt.Add(time.Hour)), trip.ErrTripCompleted)
	require.Equal(t, completed, value, "a rejected transition must not change the trip")
}

func TestFinishRejectsNonActiveStatus(t *testing.T) {
	for _, status := range []trip.Status{trip.StatusCompleted, ""} {
		t.Run(string(status), func(t *testing.T) {
			value := trip.Trip{Status: status}
			require.ErrorIs(t, value.Finish(time.Now()), trip.ErrTripCompleted)
			require.Equal(t, status, value.Status)
			require.Nil(t, value.FinishedAt)
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
