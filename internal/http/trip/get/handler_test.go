package get_test

import (
	"context"
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
	id := uuid.New()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"found", nil, http.StatusOK, ""},
		{"not found", trip.ErrTripNotFound, http.StatusNotFound, "trip_not_found"},
		{"repository error", errors.New("postgres password=secret"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Execute(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusActive}, tt.err)
			route := chi.NewRouteContext()
			route.URLParams.Add("tripId", id.String())
			request := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil)
			request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
			recorder := httptest.NewRecorder()
			get.New(uc).ServeHTTP(recorder, request)
			require.Equal(t, tt.status, recorder.Code)
			if tt.err != nil {
				require.Contains(t, recorder.Body.String(), `"code":"`+tt.code+`"`)
				require.NotContains(t, recorder.Body.String(), "postgres")
				require.NotContains(t, recorder.Body.String(), "secret")
				return
			}
			var body api.Trip
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, id, body.Id)
		})
	}
}

func TestGetTripInvalidID(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	route := chi.NewRouteContext()
	route.URLParams.Add("tripId", "invalid")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/trips/invalid", nil)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	recorder := httptest.NewRecorder()
	get.New(uc).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"invalid_request"`)
}
