package create_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/http/position/create"
	"github.com/npapaHAHA/tripgo/internal/model"
)

const (
	jsonContentType = "application/json"
	positionRoute   = "/api/v1/trips/{tripId}/positions"
	validRequest    = `{"coordinates":{"latitude":59.935,"longitude":30.335},"recorded_at":"2026-11-20T12:00:00Z"}`
)

func validInput() model.CreatePositionInput {
	return model.CreatePositionInput{
		TripID:      uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816"),
		Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335},
		RecordedAt:  time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestCreatePosition(t *testing.T) {
	input := validInput()
	value := trip.Position{ID: 42, TripID: input.TripID, Coordinates: input.Coordinates, RecordedAt: input.RecordedAt}
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), input).Return(value, nil)
	router := chi.NewRouter()
	router.Post(positionRoute, create.New(uc).ServeHTTP)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+input.TripID.String()+"/positions", strings.NewReader(validRequest))
	request.Header.Set("Content-Type", jsonContentType)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, jsonContentType, recorder.Header().Get("Content-Type"))
	var body api.TripPosition
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, value.ID, body.Id)
	require.Equal(t, value.TripID, body.TripId)
	require.Equal(t, value.Coordinates.Latitude, body.Coordinates.Latitude)
	require.Equal(t, value.Coordinates.Longitude, body.Coordinates.Longitude)
	require.True(t, value.RecordedAt.Equal(body.RecordedAt))
}

func TestCreatePositionErrors(t *testing.T) {
	input := validInput()
	tests := []struct {
		name       string
		setup      func(*Mockusecase)
		wantStatus int
		wantCode   string
	}{
		{
			name: "invalid recorded time",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), input).Return(trip.Position{}, &trip.ValidationError{Field: "recorded_at", Message: "is in the future"})
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name: "missing trip",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), input).Return(trip.Position{}, trip.ErrTripNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			name: "completed trip",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), input).Return(trip.Position{}, trip.ErrTripCompleted)
			},
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
		},
		{
			name: "storage failed",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), input).Return(trip.Position{}, errors.New("postgres password=secret"))
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
			router.Post(positionRoute, create.New(uc).ServeHTTP)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+input.TripID.String()+"/positions", strings.NewReader(validRequest))
			request.Header.Set("Content-Type", jsonContentType)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

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

func TestCreatePositionInvalidRequest(t *testing.T) {
	tests := []struct {
		name, body, contentType string
	}{
		{name: "wrong content type", body: validRequest, contentType: "text/plain"},
		{name: "invalid json", body: `{`, contentType: jsonContentType},
		{name: "null body", body: `null`, contentType: jsonContentType},
		{name: "extra object", body: validRequest + `{}`, contentType: jsonContentType},
		{name: "unknown field", body: strings.Replace(validRequest, `"coordinates":`, `"extra":true,"coordinates":`, 1), contentType: jsonContentType},
		{name: "unknown nested field", body: strings.Replace(validRequest, `"latitude":`, `"extra":true,"latitude":`, 1), contentType: jsonContentType},
		{name: "missing coordinates", body: `{"recorded_at":"2026-11-20T12:00:00Z"}`, contentType: jsonContentType},
		{name: "null coordinates", body: `{"coordinates":null,"recorded_at":"2026-11-20T12:00:00Z"}`, contentType: jsonContentType},
		{name: "missing latitude", body: strings.Replace(validRequest, `"latitude":59.935,`, "", 1), contentType: jsonContentType},
		{name: "missing longitude", body: strings.Replace(validRequest, `,"longitude":30.335`, "", 1), contentType: jsonContentType},
		{name: "missing recorded time", body: `{"coordinates":{"latitude":0,"longitude":0}}`, contentType: jsonContentType},
		{name: "null recorded time", body: strings.Replace(validRequest, `"2026-11-20T12:00:00Z"`, `null`, 1), contentType: jsonContentType},
		{name: "unparseable time", body: strings.Replace(validRequest, `2026-11-20T12:00:00Z`, `yesterday`, 1), contentType: jsonContentType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			router := chi.NewRouter()
			router.Post(positionRoute, create.New(uc).ServeHTTP)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+validInput().TripID.String()+"/positions", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", tt.contentType)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			var body api.Problem
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, "invalid_request", body.Code)
		})
	}
}

func TestCreatePositionInvalidTripID(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	router := chi.NewRouter()
	router.Post(positionRoute, create.New(uc).ServeHTTP)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/trips/invalid/positions", nil))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"invalid_request"`)
}
