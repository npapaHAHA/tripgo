package domainerror_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", trip.ErrTripNotFound, http.StatusNotFound, "trip_not_found"},
		{"completed", fmt.Errorf("finish: %w", trip.ErrTripCompleted), http.StatusConflict, "trip_completed"},
		{"busy driver", trip.ErrDriverBusy, http.StatusConflict, "driver_busy"},
		{"validation", &trip.ValidationError{Field: "price", Message: "must not be negative"}, http.StatusBadRequest, "invalid_request"},
		{"unknown", errors.New("database password=secret SELECT trips"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := domainerror.Problem(tt.err)
			require.Equal(t, tt.status, status)
			require.Equal(t, tt.status, int(body.Status))
			require.Equal(t, tt.code, body.Code)
			require.NotEmpty(t, body.Type)
			require.NotEmpty(t, body.Title)
			require.NotNil(t, body.Detail)
			if status == http.StatusInternalServerError {
				require.NotContains(t, *body.Detail, "password")
				require.NotContains(t, *body.Detail, "SELECT")
			}
		})
	}
}

func TestWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/trips/missing", nil)
	domainerror.Write(recorder, request, trip.ErrTripNotFound)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), `"instance":"/api/v1/trips/missing"`)
}

func TestWriteStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	domainerror.WriteStatus(recorder, request, http.StatusNotFound, "not_found")
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "not_found", body["code"])
	require.Equal(t, "/unknown", body["instance"])
	require.Equal(t, "https://tripgo.example/problems/not-found", body["type"])
}
