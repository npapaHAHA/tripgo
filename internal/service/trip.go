package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/database"
	"github.com/npapaHAHA/tripgo/internal/trip"
)

type TripService struct {
	repository   *database.TripRepository
	txManager    database.TxManager
	queryTimeout time.Duration
}

func NewTripService(
	repository *database.TripRepository,
	txManager database.TxManager,
	queryTimeout time.Duration,
) *TripService {
	return &TripService{
		repository:   repository,
		txManager:    txManager,
		queryTimeout: queryTimeout,
	}
}

func (s *TripService) Create(ctx context.Context, input trip.CreateInput) (trip.Trip, error) {
	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	value := trip.Trip{
		ID:         uuid.New(),
		UserID:     input.UserID,
		DriverID:   input.DriverID,
		StartPoint: input.StartPoint,
		EndPoint:   input.EndPoint,
		Price:      input.Price,
		Status:     trip.StatusActive,
		StartedAt:  time.Now().UTC(),
	}

	err := s.txManager.Do(queryCtx, func(ctx context.Context) error {
		if err := s.repository.Create(ctx, value); err != nil {
			return err
		}

		return s.repository.AddStatusHistory(
			ctx,
			value.ID,
			nil,
			trip.StatusActive,
			"trip created",
		)
	})
	if err != nil {
		return trip.Trip{}, fmt.Errorf("create trip: %w", err)
	}

	return value, nil
}

func (s *TripService) Get(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	value, err := s.repository.GetByID(queryCtx, id)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return value, nil
}

func (s *TripService) Finish(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	var value trip.Trip

	err := s.txManager.Do(queryCtx, func(ctx context.Context) error {
		finishedAt := time.Now().UTC()

		updated, err := s.repository.Finish(ctx, id, finishedAt)
		if err != nil {
			return err
		}
		value = updated

		fromStatus := trip.StatusActive
		return s.repository.AddStatusHistory(
			ctx,
			id,
			&fromStatus,
			trip.StatusCompleted,
			"trip completed",
		)
	})
	if err != nil {
		return trip.Trip{}, fmt.Errorf("finish trip: %w", err)
	}

	return value, nil
}
