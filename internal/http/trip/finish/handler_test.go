package finish_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/http/trip/finish"
)

func TestFinishTrip(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	finishedAt := time.Date(2026, time.November, 18, 10, 0, 0, 0, time.UTC)
	value := trip.Trip{ID: id, Status: trip.StatusCompleted, FinishedAt: &finishedAt}
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), id).Return(value, nil)
	router := chi.NewRouter()
	router.Post("/api/v1/trips/{tripId}/finish", finish.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	var body api.Trip
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, id, body.Id)
	require.Equal(t, api.Completed, body.Status)
	require.NotNil(t, body.FinishedAt)
	require.True(t, finishedAt.Equal(*body.FinishedAt))
}

func TestFinishTripErrors(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	tests := []struct {
		name       string
		setup      func(*Mockusecase)
		wantStatus int
		wantCode   string
	}{
		{
			name: "already completed",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripCompleted)
			},
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
		},
		{
			name: "not found",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			name: "repository error",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), id).Return(trip.Trip{}, errors.New("postgres password=secret"))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			tt.setup(uc)
			router := chi.NewRouter()
			router.Post("/api/v1/trips/{tripId}/finish", finish.New(uc).ServeHTTP)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil))

			require.Equal(t, tt.wantStatus, recorder.Code)
			require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
			var body api.Problem
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, tt.wantCode, body.Code)
			require.NotContains(t, recorder.Body.String(), "postgres")
			require.NotContains(t, recorder.Body.String(), "secret")
		})
	}
}

func TestFinishTripInvalidID(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	router := chi.NewRouter()
	router.Post("/api/v1/trips/{tripId}/finish", finish.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/trips/invalid/finish", nil))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	var body api.Problem
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "invalid_request", body.Code)
}
