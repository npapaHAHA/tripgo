package list_test

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
	"github.com/npapaHAHA/tripgo/internal/http/position/list"
)

const positionRoute = "/api/v1/trips/{tripId}/positions"

func TestListPositions(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	recordedAt := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	positions := []trip.Position{
		{ID: 1, TripID: id, Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335}, RecordedAt: recordedAt},
		{ID: 2, TripID: id, Coordinates: trip.Coordinates{Latitude: 60, Longitude: 30}, RecordedAt: recordedAt},
	}
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), id).Return(positions, nil)
	router := chi.NewRouter()
	router.Get(positionRoute, list.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	var body api.TripPositionsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Len(t, body.Positions, 2)
	require.Equal(t, int64(1), body.Positions[0].Id)
	require.Equal(t, int64(2), body.Positions[1].Id)
	require.Equal(t, id, body.Positions[0].TripId)
	require.Equal(t, positions[0].Coordinates.Latitude, body.Positions[0].Coordinates.Latitude)
	require.True(t, recordedAt.Equal(body.Positions[0].RecordedAt))
}

func TestListPositionsEmpty(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), id).Return(nil, nil)
	router := chi.NewRouter()
	router.Get(positionRoute, list.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"positions":[]}`, recorder.Body.String())
}

func TestListPositionsErrors(t *testing.T) {
	id := uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816")
	tests := []struct {
		name       string
		setup      func(*Mockusecase)
		wantStatus int
		wantCode   string
	}{
		{
			name: "missing trip",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), id).Return(nil, trip.ErrTripNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			name: "storage failed",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), id).Return(nil, errors.New("postgres password=secret"))
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
			router.Get(positionRoute, list.New(uc).ServeHTTP)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil))

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

func TestListPositionsInvalidTripID(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	router := chi.NewRouter()
	router.Get(positionRoute, list.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/invalid/positions", nil))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"invalid_request"`)
}
