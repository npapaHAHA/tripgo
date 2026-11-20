package positionhttp

import (
	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
)

func ToAPI(value trip.Position) api.TripPosition {
	return api.TripPosition{
		Id: value.ID, TripId: value.TripID,
		Coordinates: api.Coordinates{Latitude: value.Coordinates.Latitude, Longitude: value.Coordinates.Longitude},
		RecordedAt:  value.RecordedAt,
	}
}
