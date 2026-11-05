//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/npapaHAHA/tripgo/internal/config"
	"github.com/npapaHAHA/tripgo/internal/infra/postgres"
	"github.com/npapaHAHA/tripgo/internal/infra/telemetry"
)

func TestPoolInstrumentsDatabaseQuery(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	require.NotEmpty(t, databaseURL, "set TEST_DATABASE_URL or DATABASE_URL and apply migrations")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	recorder := tracetest.NewSpanRecorder()
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	tel, err := telemetry.New(ctx, traceProvider, meterProvider)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, tel.Shutdown(context.Background()))
	})

	poolConfig, err := postgres.PoolConfig(config.DatabaseConfig{
		URL: databaseURL, MaxConns: 2, MaxConnLifetime: time.Hour, ConnectTimeout: 5 * time.Second,
	}, traceProvider, tel)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	queryCtx, parent := traceProvider.Tracer("integration-test").Start(ctx, "read trips")
	defer parent.End()
	const parameter = "private-query-parameter"
	rows, err := pool.Query(queryCtx, "SELECT id FROM trips WHERE false AND $1::text <> ''", parameter)
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
	rows.Close()

	var querySpan sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		for _, attr := range span.Attributes() {
			require.NotContains(t, attr.Value.String(), parameter)
			require.NotEqual(t, "db.statement", string(attr.Key))
			require.NotEqual(t, "db.query.text", string(attr.Key))
			if attr.Key == "db.operation.name" && attr.Value.AsString() == "SELECT" {
				querySpan = span
			}
		}
		require.NotContains(t, span.Name(), parameter)
	}
	require.NotNil(t, querySpan, "the pool must produce a SELECT span")
	require.Equal(t, parent.SpanContext().SpanID(), querySpan.Parent().SpanID())
	hasCollection := false
	for _, attr := range querySpan.Attributes() {
		if attr.Key == "db.collection.name" {
			require.Equal(t, "trips", attr.Value.AsString())
			hasCollection = true
		}
	}
	require.True(t, hasCollection, "the database span must identify the trips table")

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))
	found := false
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "db.client.operation.duration" {
				continue
			}
			duration, ok := metric.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, point := range duration.DataPoints {
				operation, ok := point.Attributes.Value("db.operation.name")
				require.True(t, ok)
				collection, ok := point.Attributes.Value("db.collection.name")
				require.True(t, ok)
				if operation.AsString() == "SELECT" && collection.AsString() == "trips" {
					require.Equal(t, uint64(1), point.Count)
					found = true
				}
			}
		}
	}
	require.True(t, found, "the pool must measure the SELECT duration")
}
