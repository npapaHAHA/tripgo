package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/npapaHAHA/tripgo/internal/http/domainerror"
)

type requestState struct {
	writer    chimiddleware.WrapResponseWriter
	ctx       context.Context
	route     string
	panicking bool
}

type stateKey struct{}

func (s *requestState) status() int {
	if s.panicking {
		return http.StatusInternalServerError
	}
	if status := s.writer.Status(); status != 0 {
		return status
	}
	return http.StatusOK
}

// Wrap installs Recover, Tracing, Logging and Metrics, in that order.
func Wrap(next http.Handler, logger *slog.Logger, provider trace.TracerProvider, meter metric.Meter) (http.Handler, error) {
	duration, err := meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))
	if err != nil {
		return nil, fmt.Errorf("create HTTP duration histogram: %w", err)
	}
	active, err := meter.Int64UpDownCounter("http.server.active_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("create active HTTP requests counter: %w", err)
	}

	metrics := metricsHandler(next, duration, active)
	logging := loggingHandler(metrics, logger)
	tracing := otelhttp.NewHandler(traceHandler(logging), "HTTP",
		otelhttp.WithTracerProvider(provider),
		// Metrics below use the exact names and labels required by the course.
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + state(r).route
		}),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := &requestState{
			writer: chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor),
			ctx:    r.Context(),
			route:  matchRoute(next, r),
		}
		ctx := context.WithValue(r.Context(), stateKey{}, value)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, chi.NewRouteContext())
		r = r.WithContext(ctx)

		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(value.ctx, "panic recovered", "panic", recovered, "stack", string(debug.Stack()))
				if value.writer.Status() == 0 {
					domainerror.Write(value.writer, r, errors.New("handler panicked"))
				}
			}
		}()

		tracing.ServeHTTP(value.writer, r)
	}), nil
}

func traceHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := state(r)
		value.ctx = r.Context()
		span := trace.SpanFromContext(r.Context())

		defer func() {
			recovered := recover()
			if recovered != nil {
				value.panicking = true
				span.RecordError(fmt.Errorf("handler panic: %v", recovered))
			}
			if route := chi.RouteContext(r.Context()).RoutePattern(); route != "" {
				value.route = route
			}
			span.SetName(r.Method + " " + value.route)
			span.SetAttributes(
				attribute.String("http.route", value.route),
				attribute.Int("http.response.status_code", value.status()),
			)
			if status := value.status(); status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status))
				span.SetAttributes(attribute.String("error.type", strconv.Itoa(status)))
			}
			if recovered != nil {
				panic(recovered)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func loggingHandler(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		value := state(r)

		defer func() {
			recovered := recover()
			if recovered != nil {
				value.panicking = true
			}
			if route := chi.RouteContext(r.Context()).RoutePattern(); route != "" {
				value.route = route
			}
			level := slog.LevelInfo
			if value.status() >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			logger.Log(r.Context(), level, "request handled",
				"method", r.Method,
				"route", value.route,
				"status", value.status(),
				"duration_ms", float64(time.Since(started).Microseconds())/1000,
			)
			if recovered != nil {
				panic(recovered)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func metricsHandler(next http.Handler, duration metric.Float64Histogram, active metric.Int64UpDownCounter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		value := state(r)
		activeAttrs := metric.WithAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("http.route", value.route),
		)
		active.Add(r.Context(), 1, activeAttrs)

		defer func() {
			recovered := recover()
			if recovered != nil {
				value.panicking = true
			}
			active.Add(r.Context(), -1, activeAttrs)
			if route := chi.RouteContext(r.Context()).RoutePattern(); route != "" {
				value.route = route
			}
			duration.Record(r.Context(), time.Since(started).Seconds(), metric.WithAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("http.route", value.route),
				attribute.Int("http.response.status_code", value.status()),
			))
			if recovered != nil {
				panic(recovered)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func matchRoute(next http.Handler, r *http.Request) string {
	if routes, ok := next.(chi.Routes); ok {
		ctx := chi.NewRouteContext()
		if routes.Match(ctx, r.Method, r.URL.Path) {
			return ctx.RoutePattern()
		}
	}
	return "unknown"
}

func state(r *http.Request) *requestState {
	value, _ := r.Context().Value(stateKey{}).(*requestState)
	return value
}
