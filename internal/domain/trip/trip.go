package trip

import (
	"errors"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

type Coordinates struct {
	Latitude  float64 `validate:"gte=-90,lte=90"`
	Longitude float64 `validate:"gte=-180,lte=180"`
}

type Trip struct {
	ID             uuid.UUID
	UserID         uuid.UUID `validate:"required"`
	DriverID       uuid.UUID `validate:"required"`
	StartPoint     Coordinates
	EndPoint       Coordinates
	Price          int64 `validate:"gte=0"`
	Status         Status
	StartedAt      time.Time
	FinishedAt     *time.Time
	LastPositionAt *time.Time
}

var tripValidator = validator.New()

func New(id, userID, driverID uuid.UUID, startPoint, endPoint Coordinates, price int64, startedAt time.Time) (Trip, error) {
	value := Trip{
		ID:         id,
		UserID:     userID,
		DriverID:   driverID,
		StartPoint: startPoint,
		EndPoint:   endPoint,
		Price:      price,
		Status:     StatusActive,
		StartedAt:  startedAt.UTC().Truncate(time.Microsecond),
	}
	if err := value.Validate(); err != nil {
		return Trip{}, err
	}
	return value, nil
}

func (t *Trip) Finish(at time.Time) error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}

	t.Status = StatusCompleted
	t.FinishedAt = &at

	return nil
}

func (t Trip) Validate() error {
	err := tripValidator.Struct(t)
	if err == nil {
		return nil
	}

	var fields validator.ValidationErrors
	if !errors.As(err, &fields) {
		return err
	}

	// Report the first invalid field in declaration order.
	switch fields[0].StructNamespace() {
	case "Trip.UserID":
		return &ValidationError{Field: "user_id", Message: "must not be empty"}
	case "Trip.DriverID":
		return &ValidationError{Field: "driver_id", Message: "must not be empty"}
	case "Trip.StartPoint.Latitude":
		return &ValidationError{Field: "start_point.latitude", Message: "must be between -90 and 90"}
	case "Trip.StartPoint.Longitude":
		return &ValidationError{Field: "start_point.longitude", Message: "must be between -180 and 180"}
	case "Trip.EndPoint.Latitude":
		return &ValidationError{Field: "end_point.latitude", Message: "must be between -90 and 90"}
	case "Trip.EndPoint.Longitude":
		return &ValidationError{Field: "end_point.longitude", Message: "must be between -180 and 180"}
	case "Trip.Price":
		return &ValidationError{Field: "price", Message: "must not be negative"}
	default:
		return err
	}
}
