package create

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/model"
)

type request struct {
	UserID     *uuid.UUID   `json:"user_id"`
	DriverID   *uuid.UUID   `json:"driver_id"`
	StartPoint *coordinates `json:"start_point"`
	EndPoint   *coordinates `json:"end_point"`
	Price      *int64       `json:"price"`
}

type coordinates struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

func decode(r *http.Request) (model.CreateTripInput, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return model.CreateTripInput{}, invalid("Content-Type", "must be application/json")
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body request
	if err := decoder.Decode(&body); err != nil {
		return model.CreateTripInput{}, invalid("body", "must be a valid JSON object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return model.CreateTripInput{}, invalid("body", "must contain one JSON object")
	}
	if err := body.validateRequired(); err != nil {
		return model.CreateTripInput{}, err
	}

	value := trip.Trip{
		UserID:   *body.UserID,
		DriverID: *body.DriverID,
		StartPoint: trip.Coordinates{
			Latitude: *body.StartPoint.Latitude, Longitude: *body.StartPoint.Longitude,
		},
		EndPoint: trip.Coordinates{
			Latitude: *body.EndPoint.Latitude, Longitude: *body.EndPoint.Longitude,
		},
		Price: *body.Price,
	}
	if err := value.Validate(); err != nil {
		return model.CreateTripInput{}, err
	}
	return model.CreateTripInput{
		UserID: value.UserID, DriverID: value.DriverID,
		StartPoint: value.StartPoint, EndPoint: value.EndPoint, Price: value.Price,
	}, nil
}

func (r request) validateRequired() error {
	if r.UserID == nil {
		return invalid("user_id", "is required")
	}
	if r.DriverID == nil {
		return invalid("driver_id", "is required")
	}
	if r.Price == nil {
		return invalid("price", "is required")
	}
	if err := requireCoordinates("start_point", r.StartPoint); err != nil {
		return err
	}
	return requireCoordinates("end_point", r.EndPoint)
}

func requireCoordinates(field string, point *coordinates) error {
	if point == nil {
		return invalid(field, "is required")
	}
	if point.Latitude == nil {
		return invalid(field+".latitude", "is required")
	}
	if point.Longitude == nil {
		return invalid(field+".longitude", "is required")
	}
	return nil
}

func invalid(field, message string) error {
	return &trip.ValidationError{Field: field, Message: message}
}
