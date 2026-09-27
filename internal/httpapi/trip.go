package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/google/uuid"

	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/trip"
)

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	body, err := decodeCreateTripRequest(r)
	if err != nil {
		h.writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
		return
	}

	value, err := h.tripService.Create(r.Context(), trip.CreateInput{
		UserID:   body.UserId,
		DriverID: body.DriverId,
		StartPoint: trip.Coordinates{
			Latitude:  body.StartPoint.Latitude,
			Longitude: body.StartPoint.Longitude,
		},
		EndPoint: trip.Coordinates{
			Latitude:  body.EndPoint.Latitude,
			Longitude: body.EndPoint.Longitude,
		},
		Price: body.Price,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+value.ID.String())
	h.writeJSON(w, http.StatusCreated, toAPITrip(value))
}

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	value, err := h.tripService.Get(r.Context(), tripID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toAPITrip(value))
}

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	value, err := h.tripService.Finish(r.Context(), tripID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toAPITrip(value))
}

func decodeCreateTripRequest(r *http.Request) (api.CreateTripJSONRequestBody, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return api.CreateTripJSONRequestBody{}, fmt.Errorf("Content-Type must be application/json")
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var body api.CreateTripJSONRequestBody
	if err := decoder.Decode(&body); err != nil {
		return api.CreateTripJSONRequestBody{}, fmt.Errorf("decode request body: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return api.CreateTripJSONRequestBody{}, fmt.Errorf("request body must contain one JSON object")
	}

	if err := validateCreateTripRequest(body); err != nil {
		return api.CreateTripJSONRequestBody{}, err
	}

	return body, nil
}

func validateCreateTripRequest(body api.CreateTripJSONRequestBody) error {
	if body.UserId == uuid.Nil {
		return fmt.Errorf("user_id must not be empty")
	}
	if body.DriverId == uuid.Nil {
		return fmt.Errorf("driver_id must not be empty")
	}
	if err := validateCoordinates("start_point", body.StartPoint); err != nil {
		return err
	}
	if err := validateCoordinates("end_point", body.EndPoint); err != nil {
		return err
	}
	if body.Price < 0 {
		return fmt.Errorf("price must not be negative")
	}

	return nil
}

func validateCoordinates(name string, coordinates api.Coordinates) error {
	if coordinates.Latitude < -90 || coordinates.Latitude > 90 {
		return fmt.Errorf("%s.latitude must be between -90 and 90", name)
	}
	if coordinates.Longitude < -180 || coordinates.Longitude > 180 {
		return fmt.Errorf("%s.longitude must be between -180 and 180", name)
	}

	return nil
}

func toAPITrip(value trip.Trip) api.Trip {
	return api.Trip{
		Id:       value.ID,
		UserId:   value.UserID,
		DriverId: value.DriverID,
		StartPoint: api.Coordinates{
			Latitude:  value.StartPoint.Latitude,
			Longitude: value.StartPoint.Longitude,
		},
		EndPoint: api.Coordinates{
			Latitude:  value.EndPoint.Latitude,
			Longitude: value.EndPoint.Longitude,
		},
		Price:          value.Price,
		Status:         api.TripStatus(value.Status),
		StartedAt:      value.StartedAt,
		FinishedAt:     value.FinishedAt,
		LastPositionAt: value.LastPositionAt,
	}
}
