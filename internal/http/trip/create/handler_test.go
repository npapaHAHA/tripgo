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

const validRequest = `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":59.9398,"longitude":30.3146},"end_point":{"latitude":59.929,"longitude":30.3626},"price":1450}`

func TestCreateTrip(t *testing.T) {
	input := model.CreateTripInput{
		UserID:     uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID:   uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		StartPoint: trip.Coordinates{Latitude: 59.9398, Longitude: 30.3146},
		EndPoint:   trip.Coordinates{Latitude: 59.929, Longitude: 30.3626}, Price: 1450,
	}
	value := trip.Trip{
		ID: uuid.New(), UserID: input.UserID, DriverID: input.DriverID,
		StartPoint: input.StartPoint, EndPoint: input.EndPoint, Price: input.Price,
		Status: trip.StatusActive, StartedAt: time.Now().UTC(),
	}
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"created", nil, http.StatusCreated, ""},
		{"driver busy", trip.ErrDriverBusy, http.StatusConflict, "driver_busy"},
		{"repository error", errors.New("postgres password=secret"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMockusecase(gomock.NewController(t))
			uc.EXPECT().Execute(gomock.Any(), input).Return(value, tt.err)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validRequest))
			request.Header.Set("Content-Type", jsonContentType)
			recorder := httptest.NewRecorder()
			create.New(uc).ServeHTTP(recorder, request)
			require.Equal(t, tt.status, recorder.Code)
			if tt.err != nil {
				require.Contains(t, recorder.Body.String(), `"code":"`+tt.code+`"`)
				require.NotContains(t, recorder.Body.String(), "postgres")
				require.NotContains(t, recorder.Body.String(), "secret")
				return
			}
			require.Equal(t, "/api/v1/trips/"+value.ID.String(), recorder.Header().Get("Location"))
			require.Equal(t, jsonContentType, recorder.Header().Get("Content-Type"))
			var body api.Trip
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, value.ID, body.Id)
			require.Equal(t, api.Active, body.Status)
			require.Equal(t, input.Price, body.Price)
			require.Equal(t, input.StartPoint.Latitude, body.StartPoint.Latitude)
		})
	}
}

func TestCreateTripInvalidRequest(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{"wrong content type", validRequest, "text/plain"},
		{"broken json", "{", jsonContentType},
		{"extra object", validRequest + `{}`, jsonContentType},
		{"unknown field", strings.Replace(validRequest, `"price":1450`, `"price":1450,"other":true`, 1), jsonContentType},
		{"empty body", `{}`, jsonContentType},
		{"null body", `null`, jsonContentType},
		{"missing point", strings.Replace(validRequest, `"start_point":{"latitude":59.9398,"longitude":30.3146},`, "", 1), jsonContentType},
		{"missing coordinate", strings.Replace(validRequest, `"latitude":59.9398,`, "", 1), jsonContentType},
		{"null price", strings.Replace(validRequest, `"price":1450`, `"price":null`, 1), jsonContentType},
		{"invalid uuid", strings.Replace(validRequest, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", "invalid", 1), jsonContentType},
		{"zero uuid", strings.Replace(validRequest, "5cb72c04-7650-45c9-a79b-bcdba0631e0c", uuid.Nil.String(), 1), jsonContentType},
		{"negative price", strings.Replace(validRequest, `"price":1450`, `"price":-1`, 1), jsonContentType},
		{"latitude outside range", strings.Replace(validRequest, "59.9398", "91", 1), jsonContentType},
		{"longitude outside range", strings.Replace(validRequest, "30.3626", "181", 1), jsonContentType},
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
