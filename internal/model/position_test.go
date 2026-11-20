package model_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/model"
)

const (
	latitudeField  = "coordinates.latitude"
	longitudeField = "coordinates.longitude"
)

func TestCreatePositionInputValidate(t *testing.T) {
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		coordinates trip.Coordinates
		recordedAt  time.Time
		wantField   string
	}{
		{name: "zero coordinates", recordedAt: now},
		{name: "minimum coordinates", coordinates: trip.Coordinates{Latitude: -90, Longitude: -180}, recordedAt: now},
		{name: "maximum coordinates", coordinates: trip.Coordinates{Latitude: 90, Longitude: 180}, recordedAt: now},
		{name: "old coordinate", recordedAt: now.Add(-24 * time.Hour)},
		{name: "one minute ahead", recordedAt: now.Add(time.Minute)},
		{name: "too far ahead", recordedAt: now.Add(time.Minute + time.Nanosecond), wantField: "recorded_at"},
		{name: "missing time", wantField: "recorded_at"},
		{name: "latitude below range", coordinates: trip.Coordinates{Latitude: -91}, recordedAt: now, wantField: latitudeField},
		{name: "latitude above range", coordinates: trip.Coordinates{Latitude: 91}, recordedAt: now, wantField: latitudeField},
		{name: "longitude below range", coordinates: trip.Coordinates{Longitude: -181}, recordedAt: now, wantField: longitudeField},
		{name: "longitude above range", coordinates: trip.Coordinates{Longitude: 181}, recordedAt: now, wantField: longitudeField},
		{name: "not a number", coordinates: trip.Coordinates{Latitude: math.NaN()}, recordedAt: now, wantField: latitudeField},
		{name: "infinity", coordinates: trip.Coordinates{Longitude: math.Inf(1)}, recordedAt: now, wantField: longitudeField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := model.CreatePositionInput{Coordinates: tt.coordinates, RecordedAt: tt.recordedAt}
			err := input.Validate(now)
			if tt.wantField == "" {
				require.NoError(t, err)
				return
			}
			var validation *trip.ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, tt.wantField, validation.Field)
		})
	}
}
