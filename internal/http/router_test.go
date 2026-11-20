package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	api "github.com/npapaHAHA/tripgo/internal/generated"
	httpapi "github.com/npapaHAHA/tripgo/internal/http"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestHealthDoesNotCheckDatabase(t *testing.T) {
	db := NewMockreadinessChecker(gomock.NewController(t))
	router := httpapi.NewRouter(nil, nil, nil, db, time.Second, discardLogger())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"ok"}`, recorder.Body.String())
}

func TestReady(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{"database available", nil, http.StatusOK, `{"status":"ok"}`},
		{"database unavailable", errors.New("connection refused"), http.StatusServiceUnavailable, `{"status":"unavailable"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewMockreadinessChecker(gomock.NewController(t))
			db.EXPECT().Ping(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), time.Second)
				return tt.err
			})
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			router := httpapi.NewRouter(nil, nil, nil, db, time.Second, logger)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
			require.Equal(t, tt.status, recorder.Code)
			require.JSONEq(t, tt.body, recorder.Body.String())
			if tt.err != nil {
				require.Contains(t, logs.String(), tt.err.Error())
				require.Equal(t, 1, strings.Count(logs.String(), `"msg":"database readiness failed"`))
			} else {
				require.Empty(t, logs.String())
			}
		})
	}
}

func TestRouterRejectsInvalidTripID(t *testing.T) {
	router := httpapi.NewRouter(nil, nil, nil, nil, time.Second, discardLogger())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/trips/invalid", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), `"code":"invalid_request"`)
}

func TestRouterErrors(t *testing.T) {
	tests := []struct {
		name, method, path, code string
		status                   int
	}{
		{"unknown route", http.MethodGet, "/unknown", "not_found", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/health", "method_not_allowed", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := httpapi.NewRouter(nil, nil, nil, nil, time.Second, discardLogger())
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))
			require.Equal(t, tt.status, recorder.Code)
			require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
			var body api.Problem
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, tt.code, body.Code)
			require.Equal(t, tt.status, int(body.Status))
			require.Equal(t, tt.path, *body.Instance)
		})
	}
}
