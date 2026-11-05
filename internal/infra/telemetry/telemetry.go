package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const scopeName = "github.com/npapaHAHA/tripgo"

type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	Tracer         trace.Tracer
	Meter          metric.Meter
	created        metric.Int64Counter
	completed      metric.Int64Counter
	active         metric.Int64ObservableGauge
	dbDuration     metric.Float64Histogram
}

func Setup(ctx context.Context, logger *slog.Logger) (*Telemetry, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "trip-service"),
			attribute.String("service.version", version()),
			attribute.String("service.namespace", "tripgo"),
			attribute.String("deployment.environment", "local"),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}

	traceExporter, metricExporter, err := newExporters(ctx)
	if err != nil {
		return nil, err
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)

	value, err := New(ctx, traceProvider, meterProvider)
	if err != nil {
		return nil, errors.Join(err, meterProvider.Shutdown(ctx), traceProvider.Shutdown(ctx))
	}

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.WarnContext(ctx, "telemetry export failed", "error", err)
	}))

	return value, nil
}

// New also allows tests to use in-memory exporters and readers.
func New(ctx context.Context, traceProvider *sdktrace.TracerProvider, meterProvider *sdkmetric.MeterProvider) (*Telemetry, error) {
	meter := meterProvider.Meter(scopeName)
	created, err := meter.Int64Counter("tripgo.trips.created", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("create trips counter: %w", err)
	}
	completed, err := meter.Int64Counter("tripgo.trips.completed", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("create completed trips counter: %w", err)
	}
	active, err := meter.Int64ObservableGauge("tripgo.trips.active", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("create active trips gauge: %w", err)
	}
	dbDuration, err := meter.Float64Histogram("db.client.operation.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create database duration histogram: %w", err)
	}
	created.Add(ctx, 0)
	completed.Add(ctx, 0)

	return &Telemetry{
		TracerProvider: traceProvider,
		MeterProvider:  meterProvider,
		Tracer:         traceProvider.Tracer(scopeName),
		Meter:          meter,
		created:        created,
		completed:      completed,
		active:         active,
		dbDuration:     dbDuration,
	}, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	metricsErr := t.MeterProvider.Shutdown(ctx)
	tracesErr := t.TracerProvider.Shutdown(ctx)
	return errors.Join(metricsErr, tracesErr)
}

func (t *Telemetry) TripCreated(ctx context.Context) {
	t.created.Add(ctx, 1)
}

func (t *Telemetry) TripCompleted(ctx context.Context) {
	t.completed.Add(ctx, 1)
}

func (t *Telemetry) RegisterActiveTrips(count func(context.Context) (int64, error)) error {
	_, err := t.Meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		value, err := count(ctx)
		if err != nil {
			return fmt.Errorf("count active trips: %w", err)
		}
		observer.ObserveInt64(t.active, value)
		return nil
	}, t.active)
	if err != nil {
		return fmt.Errorf("register active trips gauge: %w", err)
	}
	return nil
}

func (t *Telemetry) DBOperation(ctx context.Context, operation, collection string, elapsed time.Duration, err error) {
	errorType := ""
	if err != nil {
		errorType = fmt.Sprintf("%T", err)
	}
	t.dbDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		attribute.String("db.operation.name", operation),
		attribute.String("db.collection.name", collection),
		attribute.String("error.type", errorType),
	))
}

func newExporters(ctx context.Context) (sdktrace.SpanExporter, sdkmetric.Exporter, error) {
	var (
		traceExporter  sdktrace.SpanExporter
		metricExporter sdkmetric.Exporter
		err            error
	)

	switch protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"); protocol {
	case "", "grpc":
		traceExporter, err = otlptracegrpc.New(ctx)
		if err == nil {
			metricExporter, err = otlpmetricgrpc.New(ctx)
		}
	case "http/protobuf":
		traceExporter, err = otlptracehttp.New(ctx)
		if err == nil {
			metricExporter, err = otlpmetrichttp.New(ctx)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported OTEL_EXPORTER_OTLP_PROTOCOL %q", protocol)
	}

	if err != nil {
		if traceExporter != nil {
			err = errors.Join(err, traceExporter.Shutdown(ctx))
		}
		return nil, nil, fmt.Errorf("create OTLP exporters: %w", err)
	}
	return traceExporter, metricExporter, nil
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return "dev"
}
