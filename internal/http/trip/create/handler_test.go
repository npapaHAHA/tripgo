package create_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/http/trip/create"
	"github.com/npapaHAHA/tripgo/internal/model"
)

const jsonContentType = "application/json"

const validRequest = `{
	"user_id": "5cb72c04-7650-45c9-a79b-bcdba0631e0c",
	"driver_id": "8860b315-ec86-42eb-a17c-7c163d721ff5",
	"start_point": {"latitude": 59.9398, "longitude": 30.3146},
	"end_point": {"latitude": 59.929, "longitude": 30.3626},
	"price": 1450
}`

func TestCreateTrip(t *testing.T) {
	input := validInput()
	value := trip.Trip{
		ID:         uuid.MustParse("b7a855a7-3436-4a36-a50e-f76142080816"),
		UserID:     input.UserID,
		DriverID:   input.DriverID,
		StartPoint: input.StartPoint,
		EndPoint:   input.EndPoint,
		Price:      input.Price,
		Status:     trip.StatusActive,
		StartedAt:  time.Date(2026, time.November, 18, 10, 0, 0, 0, time.UTC),
	}
	uc := NewMockusecase(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), input).Return(value, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validRequest))
	request.Header.Set("Content-Type", jsonContentType)
	recorder := httptest.NewRecorder()

	create.New(uc).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, "/api/v1/trips/"+value.ID.String(), recorder.Header().Get("Location"))
	require.Equal(t, jsonContentType, recorder.Header().Get("Content-Type"))
	var body api.Trip
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, value.ID, body.Id)
	require.Equal(t, input.UserID, body.UserId)
	require.Equal(t, input.DriverID, body.DriverId)
	require.Equal(t, api.Active, body.Status)
	require.Equal(t, input.Price, body.Price)
	require.Equal(t, input.StartPoint.Latitude, body.StartPoint.Latitude)
	require.Equal(t, input.StartPoint.Longitude, body.StartPoint.Longitude)
	require.Equal(t, input.EndPoint.Latitude, body.EndPoint.Latitude)
	require.Equal(t, input.EndPoint.Longitude, body.EndPoint.Longitude)
	require.True(t, value.StartedAt.Equal(body.StartedAt))
	require.Nil(t, body.FinishedAt)
}

func TestCreateTripErrors(t *testing.T) {
	input := validInput()
	tests := []struct {
		name       string
		setup      func(*Mockusecase)
		wantStatus int
		wantCode   string
	}{
		{
			name: "driver busy",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), input).Return(trip.Trip{}, trip.ErrDriverBusy)
			},
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
		},
		{
			name: "repository error",
			setup: func(uc *Mockusecase) {
				uc.EXPECT().Execute(gomock.Any(), input).Return(trip.Trip{}, errors.New("postgres password=secret"))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			tt.setup(uc)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validRequest))
			request.Header.Set("Content-Type", jsonContentType)
			recorder := httptest.NewRecorder()

			create.New(uc).ServeHTTP(recorder, request)

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

func validInput() model.CreateTripInput {
	return model.CreateTripInput{
		UserID:     uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID:   uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		StartPoint: trip.Coordinates{Latitude: 59.9398, Longitude: 30.3146},
		EndPoint:   trip.Coordinates{Latitude: 59.929, Longitude: 30.3626},
		Price:      1450,
	}
}

func TestCreateTripInvalidRequest(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{
			name:        "wrong content type",
			body:        validRequest,
			contentType: "text/plain",
		},
		{
			name:        "broken json",
			body:        "{",
			contentType: jsonContentType,
		},
		{
			name:        "extra object",
			body:        validRequest + `{}`,
			contentType: jsonContentType,
		},
		{
			name:        "unknown field",
			body:        strings.Replace(validRequest, `"price": 1450`, `"price": 1450, "other": true`, 1),
			contentType: jsonContentType,
		},
		{
			name:        "empty body",
			body:        `{}`,
			contentType: jsonContentType,
		},
		{
			name:        "null body",
			body:        `null`,
			contentType: jsonContentType,
		},
		{
			name:        "missing point",
			body:        strings.Replace(validRequest, `"start_point": {"latitude": 59.9398, "longitude": 30.3146},`, "", 1),
			contentType: jsonContentType,
		},
		{
			name:        "missing coordinate",
			body:        strings.Replace(validRequest, `"latitude": 59.9398, `, "", 1),
			contentType: jsonContentType,
		},
		{
			name:        "null price",
			body:        strings.Replace(validRequest, `"price": 1450`, `"price": null`, 1),
			contentType: jsonContentType,
		},
		{
			name:        "invalid uuid",
			body:        strings.Replace(validRequest, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", "invalid", 1),
			contentType: jsonContentType,
		},
		{
			name:        "zero uuid",
			body:        strings.Replace(validRequest, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", uuid.Nil.String(), 1),
			contentType: jsonContentType,
		},
		{
			name:        "negative price",
			body:        strings.Replace(validRequest, `"price": 1450`, `"price": -1`, 1),
			contentType: jsonContentType,
		},
		{
			name:        "latitude outside range",
			body:        strings.Replace(validRequest, "59.9398", "91", 1),
			contentType: jsonContentType,
		},
		{
			name:        "longitude outside range",
			body:        strings.Replace(validRequest, "30.3626", "181", 1),
			contentType: jsonContentType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", tt.contentType)
			recorder := httptest.NewRecorder()

			create.New(uc).ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
			var body api.Problem
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, "invalid_request", body.Code)
		})
	}
}
