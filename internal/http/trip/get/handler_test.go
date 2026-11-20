package get_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/http/trip/get"
)

func TestGetTrip(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusActive}, nil)
	router := chi.NewRouter()
	router.Get("/api/v1/trips/{tripId}", get.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	var body api.Trip
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, id, body.Id)
	require.Equal(t, api.Active, body.Status)
}

func TestGetTripErrors(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	tests := []struct {
		name       string
		setup      func(*Mockusecase)
		wantStatus int
		wantCode   string
	}{
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
			router.Get("/api/v1/trips/{tripId}", get.New(uc).ServeHTTP)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil))

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

func TestGetTripInvalidID(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	router := chi.NewRouter()
	router.Get("/api/v1/trips/{tripId}", get.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/invalid", nil))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	var body api.Problem
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "invalid_request", body.Code)
}
