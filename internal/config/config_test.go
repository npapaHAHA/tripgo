package config_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/npapaHAHA/tripgo/internal/config"
)

const (
	maxConnsEnv = "DATABASE_MAX_CONNS"
	minConnsEnv = "DATABASE_MIN_CONNS"
)

func validEnv() map[string]string {
	return map[string]string{
		"HTTP_ADDR":                   ":8080",
		"HTTP_READ_TIMEOUT":           "10s",
		"HTTP_READ_HEADER_TIMEOUT":    "5s",
		"HTTP_WRITE_TIMEOUT":          "15s",
		"HTTP_IDLE_TIMEOUT":           "60s",
		"LOG_LEVEL":                   "debug",
		"SHUTDOWN_TIMEOUT":            "10s",
		"DATABASE_URL":                "postgres://localhost/tripgo",
		maxConnsEnv:                   "10",
		minConnsEnv:                   "2",
		"DATABASE_MAX_CONN_LIFETIME":  "30m",
		"DATABASE_CONNECT_TIMEOUT":    "5s",
		"DATABASE_QUERY_TIMEOUT":      "3s",
		"OTEL_SERVICE_NAME":           "trip-service",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
		"OTEL_TRACES_SAMPLER":         "parentbased_always_on",
		"OTEL_TRACES_SAMPLER_ARG":     "0.5",
	}
}

func setValidEnv(t *testing.T) {
	t.Helper()
	for name, value := range validEnv() {
		t.Setenv(name, value)
	}
}

func TestLoad(t *testing.T) {
	setValidEnv(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTP.Addr)
	require.Equal(t, 10*time.Second, cfg.HTTP.ReadTimeout)
	require.Equal(t, 5*time.Second, cfg.HTTP.ReadHeaderTimeout)
	require.Equal(t, 15*time.Second, cfg.HTTP.WriteTimeout)
	require.Equal(t, time.Minute, cfg.HTTP.IdleTimeout)
	require.Equal(t, slog.LevelDebug, cfg.LogLevel)
	require.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	require.Equal(t, "postgres://localhost/tripgo", cfg.Database.URL)
	require.Equal(t, int32(10), cfg.Database.MaxConns)
	require.Equal(t, int32(2), cfg.Database.MinConns)
	require.Equal(t, 30*time.Minute, cfg.Database.MaxConnLifetime)
	require.Equal(t, 5*time.Second, cfg.Database.ConnectTimeout)
	require.Equal(t, 3*time.Second, cfg.Database.QueryTimeout)
}

func TestLoadRequiresEnvironment(t *testing.T) {
	for name := range validEnv() {
		if name == "OTEL_TRACES_SAMPLER_ARG" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			setValidEnv(t)
			require.NoError(t, os.Unsetenv(name))
			_, err := config.Load()
			require.ErrorContains(t, err, name)
			require.ErrorContains(t, err, "is not set")
		})
	}
}

func TestLoadRejectsEmptyEnvironment(t *testing.T) {
	for _, name := range []string{"HTTP_ADDR", "DATABASE_URL", "OTEL_SERVICE_NAME", "OTEL_EXPORTER_OTLP_ENDPOINT"} {
		t.Run(name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(name, " \t ")
			_, err := config.Load()
			require.ErrorContains(t, err, name)
			require.ErrorContains(t, err, "is empty")
		})
	}
}

func TestLoadRejectsInvalidDurations(t *testing.T) {
	names := []string{
		"HTTP_READ_TIMEOUT", "HTTP_READ_HEADER_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT",
		"SHUTDOWN_TIMEOUT", "DATABASE_MAX_CONN_LIFETIME", "DATABASE_CONNECT_TIMEOUT", "DATABASE_QUERY_TIMEOUT",
	}
	for _, name := range names {
		for _, value := range []string{"0s", "-1s", "not-a-duration"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				setValidEnv(t)
				t.Setenv(name, value)
				_, err := config.Load()
				require.ErrorContains(t, err, name)
			})
		}
	}
}

func TestLoadRejectsInvalidConnections(t *testing.T) {
	tests := []struct {
		name, value, errorField string
	}{
		{maxConnsEnv, "0", maxConnsEnv},
		{maxConnsEnv, "-1", maxConnsEnv},
		{maxConnsEnv, "1", minConnsEnv},
		{maxConnsEnv, "many", maxConnsEnv},
		{maxConnsEnv, "2147483648", maxConnsEnv},
		{minConnsEnv, "-1", minConnsEnv},
		{minConnsEnv, "11", minConnsEnv},
		{minConnsEnv, "few", minConnsEnv},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tt.name, tt.value)
			_, err := config.Load()
			require.ErrorContains(t, err, tt.errorField)
		})
	}
}

func TestLoadAllowsZeroMinConnections(t *testing.T) {
	setValidEnv(t)
	t.Setenv(minConnsEnv, "0")
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Zero(t, cfg.Database.MinConns)
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	setValidEnv(t)
	t.Setenv("LOG_LEVEL", "verbose")
	_, err := config.Load()
	require.ErrorContains(t, err, "LOG_LEVEL")
}

func TestLoadTelemetryProtocols(t *testing.T) {
	for _, protocol := range []string{"grpc", "http/protobuf"} {
		t.Run(protocol, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
			_, err := config.Load()
			require.NoError(t, err)
		})
	}
	t.Run("unsupported", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/json")
		_, err := config.Load()
		require.ErrorContains(t, err, "OTEL_EXPORTER_OTLP_PROTOCOL")
	})
}

func TestLoadTelemetrySamplers(t *testing.T) {
	samplers := []string{
		"always_on", "always_off", "parentbased_always_on", "parentbased_always_off",
		"traceidratio", "parentbased_traceidratio",
	}
	for _, sampler := range samplers {
		t.Run(sampler, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("OTEL_TRACES_SAMPLER", sampler)
			_, err := config.Load()
			require.NoError(t, err)
		})
	}
	t.Run("unsupported", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("OTEL_TRACES_SAMPLER", "sometimes")
		_, err := config.Load()
		require.ErrorContains(t, err, "OTEL_TRACES_SAMPLER")
	})
}

func TestLoadTelemetrySamplerRatio(t *testing.T) {
	for _, ratio := range []string{"0", "0.25", "1"} {
		t.Run(ratio, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", ratio)
			_, err := config.Load()
			require.NoError(t, err)
		})
	}
	for _, ratio := range []string{"-0.1", "1.1", "NaN", "Inf", "text", ""} {
		t.Run("invalid/"+ratio, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", ratio)
			_, err := config.Load()
			require.ErrorContains(t, err, "OTEL_TRACES_SAMPLER_ARG")
		})
	}
	t.Run("SDK default ratio", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
		require.NoError(t, os.Unsetenv("OTEL_TRACES_SAMPLER_ARG"))
		_, err := config.Load()
		require.NoError(t, err)
	})
}
