package model

import (
	"errors"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

type CreatePositionInput struct {
	TripID      uuid.UUID
	Coordinates trip.Coordinates
	RecordedAt  time.Time `validate:"required"`
}

var positionValidator = validator.New()

func (input CreatePositionInput) Validate(now time.Time) error {
	if err := positionValidator.Struct(input); err != nil {
		var fields validator.ValidationErrors
		if !errors.As(err, &fields) {
			return err
		}
		switch fields[0].StructNamespace() {
		case "CreatePositionInput.Coordinates.Latitude":
			return &trip.ValidationError{Field: "coordinates.latitude", Message: "must be between -90 and 90"}
		case "CreatePositionInput.Coordinates.Longitude":
			return &trip.ValidationError{Field: "coordinates.longitude", Message: "must be between -180 and 180"}
		case "CreatePositionInput.RecordedAt":
			return &trip.ValidationError{Field: "recorded_at", Message: "is required"}
		default:
			return err
		}
	}
	if input.RecordedAt.After(now.Add(time.Minute)) {
		return &trip.ValidationError{Field: "recorded_at", Message: "must not be more than one minute in the future"}
	}
	return nil
}
