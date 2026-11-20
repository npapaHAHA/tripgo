package create

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/model"
)

type request struct {
	Coordinates *coordinates `json:"coordinates"`
	RecordedAt  *time.Time   `json:"recorded_at"`
}

type coordinates struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

func decode(r *http.Request) (model.CreatePositionInput, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return model.CreatePositionInput{}, invalid("Content-Type", "must be application/json")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body request
	if err := decoder.Decode(&body); err != nil {
		return model.CreatePositionInput{}, invalid("body", "must be a valid JSON object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return model.CreatePositionInput{}, invalid("body", "must contain one JSON object")
	}
	if err := body.validateRequired(); err != nil {
		return model.CreatePositionInput{}, err
	}
	return model.CreatePositionInput{
		Coordinates: trip.Coordinates{Latitude: *body.Coordinates.Latitude, Longitude: *body.Coordinates.Longitude},
		RecordedAt:  *body.RecordedAt,
	}, nil
}

func (r request) validateRequired() error {
	if r.Coordinates == nil {
		return invalid("coordinates", "is required")
	}
	if r.Coordinates.Latitude == nil {
		return invalid("coordinates.latitude", "is required")
	}
	if r.Coordinates.Longitude == nil {
		return invalid("coordinates.longitude", "is required")
	}
	if r.RecordedAt == nil {
		return invalid("recorded_at", "is required")
	}
	return nil
}

func invalid(field, message string) error {
	return &trip.ValidationError{Field: field, Message: message}
}
