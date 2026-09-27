package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/trip"
)

const problemTypeBase = "https://tripgo.example/problems/"

func (h *Handler) HandleRequestError(w http.ResponseWriter, r *http.Request, _ error) {
	h.writeProblem(
		w,
		r,
		http.StatusBadRequest,
		"invalid_request",
		"Invalid request",
		"Request parameters are invalid",
	)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, trip.ErrNotFound):
		h.writeProblem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "Trip was not found")
	case errors.Is(err, trip.ErrCompleted):
		h.writeProblem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "Trip is already completed")
	case errors.Is(err, trip.ErrDriverBusy):
		h.writeProblem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	default:
		h.logger.Error(
			"handle HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)
		h.writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "An unexpected error occurred")
	}
}

func (h *Handler) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code string,
	title string,
	detail string,
) {
	instance := r.URL.Path

	h.writeJSONWithContentType(w, status, "application/problem+json", api.Problem{
		Type:     problemTypeBase + problemTypeName(code),
		Title:    title,
		Status:   int32(status),
		Detail:   &detail,
		Instance: &instance,
		Code:     code,
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	h.writeJSONWithContentType(w, status, "application/json", value)
}

func (h *Handler) writeJSONWithContentType(w http.ResponseWriter, status int, contentType string, value any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		h.logger.Error("write HTTP response", "error", err)
	}
}

func problemTypeName(code string) string {
	switch code {
	case "invalid_request":
		return "invalid-request"
	case "trip_not_found":
		return "trip-not-found"
	case "trip_completed":
		return "trip-completed"
	case "driver_busy":
		return "driver-busy"
	default:
		return "internal-error"
	}
}
