package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/npapaHAHA/tripgo/internal/infra/telemetry"
)

const (
	createdMetric   = "tripgo.trips.created"
	completedMetric = "tripgo.trips.completed"
)

func TestBusinessCountersExistBeforeFirstTrip(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	traces := sdktrace.NewTracerProvider()
	value, err := telemetry.New(ctx, traces, meters)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, value.Shutdown(ctx)) })

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))
	metrics := make(map[string]metricdata.Aggregation)
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			metrics[metric.Name] = metric.Data
		}
	}
	for _, name := range []string{createdMetric, completedMetric} {
		counter, ok := metrics[name].(metricdata.Sum[int64])
		require.True(t, ok, "%s must be visible before any trip", name)
		require.True(t, counter.IsMonotonic)
		require.Len(t, counter.DataPoints, 1)
		require.Zero(t, counter.DataPoints[0].Value)
	}
}

func TestBusinessAndDatabaseMetrics(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	traces := sdktrace.NewTracerProvider()
	value, err := telemetry.New(ctx, traces, meters)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, value.Shutdown(ctx)) })
	require.NoError(t, value.RegisterActiveTrips(func(context.Context) (int64, error) { return 7, nil }))
	value.TripCreated(ctx)
	value.TripCreated(ctx)
	value.TripCompleted(ctx)
	value.DBOperation(ctx, "SELECT", "trips", 10*time.Millisecond, nil)
	value.DBOperation(ctx, "INSERT", "trips", time.Millisecond, errors.New("database unavailable"))

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))
	metrics := make(map[string]metricdata.Aggregation)
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			metrics[metric.Name] = metric.Data
		}
	}
	require.Equal(t, int64(2), metrics[createdMetric].(metricdata.Sum[int64]).DataPoints[0].Value)
	require.Equal(t, int64(1), metrics[completedMetric].(metricdata.Sum[int64]).DataPoints[0].Value)
	require.Equal(t, int64(7), metrics["tripgo.trips.active"].(metricdata.Gauge[int64]).DataPoints[0].Value)
	duration := metrics["db.client.operation.duration"].(metricdata.Histogram[float64])
	require.Len(t, duration.DataPoints, 2)
	for _, point := range duration.DataPoints {
		collection, ok := point.Attributes.Value("db.collection.name")
		require.True(t, ok)
		require.Equal(t, "trips", collection.AsString())
		operation, ok := point.Attributes.Value("db.operation.name")
		require.True(t, ok)
		errorType, ok := point.Attributes.Value("error.type")
		require.True(t, ok)
		if operation.AsString() == "INSERT" {
			require.NotEmpty(t, errorType.AsString())
		} else {
			require.Empty(t, errorType.AsString())
		}
	}
}

func TestShutdownExportsSpansCreatedByFinalMetricCollection(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	exporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(collector.URL))
	require.NoError(t, err)
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(
		sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Hour)),
	))
	recorder := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	value, err := telemetry.New(ctx, traces, meters)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = value.Shutdown(cleanupCtx)
	})
	require.NoError(t, value.RegisterActiveTrips(func(ctx context.Context) (int64, error) {
		_, span := value.Tracer.Start(ctx, "count active trips")
		span.End()
		return 0, nil
	}))

	require.NoError(t, value.Shutdown(ctx))
	spans := recorder.Ended()
	require.Len(t, spans, 1, "the final gauge query must still be traced")
	require.Equal(t, "count active trips", spans[0].Name())
}

func TestLoggerUsesSpanContextAfterWith(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(&output, nil))).With("service", "trip-service")
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	ctx, span := provider.Tracer("test").Start(context.Background(), "create trip")
	defer span.End()
	logger.InfoContext(ctx, "trip created")
	require.Contains(t, output.String(), `"service":"trip-service"`)
	require.Contains(t, output.String(), `"trace_id":"`+span.SpanContext().TraceID().String()+`"`)
	require.Contains(t, output.String(), `"span_id":"`+span.SpanContext().SpanID().String()+`"`)
}

func TestSetupDoesNotRequireRunningCollector(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := telemetry.Setup(ctx, logger)
	require.NoError(t, err)
	_, span := value.Tracer.Start(ctx, "request")
	require.False(t, span.IsRecording())
	span.End()
	require.NotPanics(t, func() {
		value.TripCreated(ctx)
		value.TripCompleted(ctx)
		value.DBOperation(ctx, "SELECT", "trips", time.Millisecond, nil)
	})
	shutdownCtx, stopShutdown := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopShutdown()
	if err := value.Shutdown(shutdownCtx); err != nil {
		t.Logf("final export to the unavailable collector: %v", err)
	}
}

func TestSetupRejectsUnsupportedProtocol(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "json")
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	_, err := telemetry.Setup(context.Background(), logger)
	require.ErrorContains(t, err, "OTEL_EXPORTER_OTLP_PROTOCOL")
}
