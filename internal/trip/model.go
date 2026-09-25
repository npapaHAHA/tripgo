package trip

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

var (
	ErrNotFound   = errors.New("trip not found")
	ErrCompleted  = errors.New("trip already completed")
	ErrDriverBusy = errors.New("driver already has an active trip")
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type CreateInput struct {
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
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
