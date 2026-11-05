package trip

import (
	"math"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type Trip struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	DriverID       uuid.UUID
	StartPoint     Coordinates
	EndPoint       Coordinates
	Price          int64
	Status         Status
	StartedAt      time.Time
	FinishedAt     *time.Time
	LastPositionAt *time.Time
}

func New(userID, driverID uuid.UUID, startPoint, endPoint Coordinates, price int64) (Trip, error) {
	value := Trip{
		ID:         uuid.New(),
		UserID:     userID,
		DriverID:   driverID,
		StartPoint: startPoint,
		EndPoint:   endPoint,
		Price:      price,
		Status:     StatusActive,
		StartedAt:  time.Now().UTC().Truncate(time.Microsecond),
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
	if t.UserID == uuid.Nil {
		return &ValidationError{Field: "user_id", Message: "must not be empty"}
	}
	if t.DriverID == uuid.Nil {
		return &ValidationError{Field: "driver_id", Message: "must not be empty"}
	}
	if err := validateCoordinates("start_point", t.StartPoint); err != nil {
		return err
	}
	if err := validateCoordinates("end_point", t.EndPoint); err != nil {
		return err
	}
	if t.Price < 0 {
		return &ValidationError{Field: "price", Message: "must not be negative"}
	}

	return nil
}

func validateCoordinates(field string, value Coordinates) error {
	if math.IsNaN(value.Latitude) || value.Latitude < -90 || value.Latitude > 90 {
		return &ValidationError{Field: field + ".latitude", Message: "must be between -90 and 90"}
	}
	if math.IsNaN(value.Longitude) || value.Longitude < -180 || value.Longitude > 180 {
		return &ValidationError{Field: field + ".longitude", Message: "must be between -180 and 180"}
	}

	return nil
}
