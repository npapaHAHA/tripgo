package domainerror

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
)

const problemTypeBase = "https://tripgo.example/problems/"

func Problem(err error) (int, api.Problem) {
	status := int32(http.StatusInternalServerError)
	code, title, detail := "internal_error", "Internal error", "An unexpected error occurred"
	var validation *trip.ValidationError

	switch {
	case errors.As(err, &validation):
		status = http.StatusBadRequest
		code, title, detail = "invalid_request", "Invalid request", validation.Error()
	case errors.Is(err, trip.ErrTripNotFound):
		status = http.StatusNotFound
		code, title, detail = "trip_not_found", "Trip not found", "Trip was not found"
	case errors.Is(err, trip.ErrTripCompleted):
		status = http.StatusConflict
		code, title, detail = "trip_completed", "Trip completed", "Trip is already completed"
	case errors.Is(err, trip.ErrDriverBusy):
		status = http.StatusConflict
		code, title, detail = "driver_busy", "Driver busy", "Driver already has an active trip"
	}

	return int(status), api.Problem{
		Type:   problemTypeBase + strings.ReplaceAll(code, "_", "-"),
		Title:  title,
		Status: status,
		Detail: &detail,
		Code:   code,
	}
}

func Write(w http.ResponseWriter, r *http.Request, err error) {
	status, body := Problem(err)
	writeProblem(w, r, status, body)
}

func WriteStatus(w http.ResponseWriter, r *http.Request, status int32, code string) {
	writeProblem(w, r, int(status), api.Problem{
		Type:   problemTypeBase + strings.ReplaceAll(code, "_", "-"),
		Title:  http.StatusText(int(status)),
		Status: status,
		Code:   code,
	})
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, body api.Problem) {
	instance := r.URL.Path
	body.Instance = &instance
	writeJSON(w, status, "application/problem+json", body)
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	writeJSON(w, status, "application/json", value)
}

func writeJSON(w http.ResponseWriter, status int, contentType string, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "cannot encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		return
	}
}
