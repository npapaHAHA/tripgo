package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	httpapi "github.com/npapaHAHA/tripgo/internal/http"
	"github.com/npapaHAHA/tripgo/internal/http/middleware"
)

const tripID = "8860b315-ec86-42eb-a17c-7c163d721ff5"

func TestWrapUsesRouteFromGeneratedRouter(t *testing.T) {
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router := httpapi.NewRouter(endpoint, endpoint, endpoint, nil, time.Second, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	handler, reader, recorder, logs := setup(t, router)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID, http.NoBody))
	require.Equal(t, "GET /api/v1/trips/{tripId}", recorder.Ended()[0].Name())
	require.Contains(t, logs.String(), `"route":"/api/v1/trips/{tripId}"`)
	metrics := collect(t, reader)
	active := metrics["http.server.active_requests"].(metricdata.Sum[int64])
	value, ok := active.DataPoints[0].Attributes.Value("http.route")
	require.True(t, ok)
	require.Equal(t, "/api/v1/trips/{tripId}", value.AsString())
}

func TestWrapRecordsTemplateStatusAndTraceParent(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			router := chi.NewRouter()
			router.Get("/api/v1/trips/{tripId}", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})
			handler, reader, recorder, logs := setup(t, router)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID, http.NoBody)
			request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			require.Equal(t, status, response.Code)
			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, "GET /api/v1/trips/{tripId}", spans[0].Name())
			require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", spans[0].SpanContext().TraceID().String())
			require.Equal(t, "00f067aa0ba902b7", spans[0].Parent().SpanID().String())
			if status == http.StatusInternalServerError {
				require.Equal(t, codes.Error, spans[0].Status().Code)
			}

			var record map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
			require.Equal(t, "request handled", record["msg"])
			require.Equal(t, "/api/v1/trips/{tripId}", record["route"])
			require.Equal(t, float64(status), record["status"])
			level := "INFO"
			if status == http.StatusInternalServerError {
				level = "ERROR"
			}
			require.Equal(t, level, record["level"])
			require.NotContains(t, logs.String(), tripID)

			metrics := collect(t, reader)
			duration := metrics["http.server.request.duration"].(metricdata.Histogram[float64])
			require.Len(t, duration.DataPoints, 1)
			require.Equal(t, uint64(1), duration.DataPoints[0].Count)
			value, ok := duration.DataPoints[0].Attributes.Value("http.route")
			require.True(t, ok)
			require.Equal(t, "/api/v1/trips/{tripId}", value.AsString())
			value, ok = duration.DataPoints[0].Attributes.Value("http.response.status_code")
			require.True(t, ok)
			require.Equal(t, int64(status), value.AsInt64())
			active := metrics["http.server.active_requests"].(metricdata.Sum[int64])
			require.Equal(t, int64(0), active.DataPoints[0].Value)
		})
	}
}

func TestWrapRecoversPanicAndRecordsFailure(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/panic", func(http.ResponseWriter, *http.Request) {
		panic("private database password")
	})
	handler, reader, recorder, logs := setup(t, router)
	response := httptest.NewRecorder()

	require.NotPanics(t, func() {
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", http.NoBody))
	})
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, "application/problem+json", response.Header().Get("Content-Type"))
	require.Contains(t, response.Body.String(), "internal_error")
	require.NotContains(t, response.Body.String(), "private database password")
	require.Equal(t, 1, strings.Count(logs.String(), `"msg":"request handled"`))
	require.Contains(t, logs.String(), `"msg":"panic recovered"`)
	require.Contains(t, logs.String(), `"stack":`)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.NotEmpty(t, spans[0].Events())
	metrics := collect(t, reader)
	duration := metrics["http.server.request.duration"].(metricdata.Histogram[float64])
	value, ok := duration.DataPoints[0].Attributes.Value("http.response.status_code")
	require.True(t, ok)
	require.Equal(t, int64(http.StatusInternalServerError), value.AsInt64())
}

func TestActiveRequestsIsMeasuredWhileRequestRuns(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	router := chi.NewRouter()
	router.Get("/waiting/{id}", func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	handler, reader, _, _ := setup(t, router)
	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/waiting/123", http.NoBody))
	}()
	<-entered
	metrics := collect(t, reader)
	active := metrics["http.server.active_requests"].(metricdata.Sum[int64])
	require.Len(t, active.DataPoints, 1)
	require.Equal(t, int64(1), active.DataPoints[0].Value)
	value, ok := active.DataPoints[0].Attributes.Value(attribute.Key("http.route"))
	require.True(t, ok)
	require.Equal(t, "/waiting/{id}", value.AsString())
	close(release)
	<-done
	metrics = collect(t, reader)
	active = metrics["http.server.active_requests"].(metricdata.Sum[int64])
	require.Equal(t, int64(0), active.DataPoints[0].Value)
}

func setup(t *testing.T, next http.Handler) (http.Handler, *sdkmetric.ManualReader, *tracetest.SpanRecorder, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := tracetest.NewSpanRecorder()
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		require.NoError(t, traceProvider.Shutdown(context.Background()))
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	handler, err := middleware.Wrap(next, logger, traceProvider, meterProvider.Meter("test"))
	require.NoError(t, err)
	return handler, reader, recorder, logs
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	result := make(map[string]metricdata.Aggregation)
	for _, scope := range data.ScopeMetrics {
		for _, value := range scope.Metrics {
			result[value.Name] = value.Data
		}
	}
	return result
}
