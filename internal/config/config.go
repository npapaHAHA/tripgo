package config

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTP            HTTPConfig
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	Database        DatabaseConfig
}

type HTTPConfig struct {
	Addr              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
}

func Load() (Config, error) {
	httpConfig, err := loadHTTP()
	if err != nil {
		return Config{}, err
	}

	databaseConfig, err := loadDatabase()
	if err != nil {
		return Config{}, err
	}

	logLevel, err := requiredLogLevel("LOG_LEVEL")
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := requiredDuration("SHUTDOWN_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	if err := validateTelemetry(); err != nil {
		return Config{}, err
	}

	return Config{
		HTTP:            httpConfig,
		Database:        databaseConfig,
		LogLevel:        logLevel,
		ShutdownTimeout: shutdownTimeout,
	}, nil
}

func loadHTTP() (HTTPConfig, error) {
	addr, err := requiredString("HTTP_ADDR")
	if err != nil {
		return HTTPConfig{}, err
	}

	readTimeout, err := requiredDuration("HTTP_READ_TIMEOUT")
	if err != nil {
		return HTTPConfig{}, err
	}
	readHeaderTimeout, err := requiredDuration("HTTP_READ_HEADER_TIMEOUT")
	if err != nil {
		return HTTPConfig{}, err
	}
	writeTimeout, err := requiredDuration("HTTP_WRITE_TIMEOUT")
	if err != nil {
		return HTTPConfig{}, err
	}
	idleTimeout, err := requiredDuration("HTTP_IDLE_TIMEOUT")
	if err != nil {
		return HTTPConfig{}, err
	}

	return HTTPConfig{
		Addr:              addr,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}, nil
}

func loadDatabase() (DatabaseConfig, error) {
	databaseURL, err := requiredString("DATABASE_URL")
	if err != nil {
		return DatabaseConfig{}, err
	}

	databaseMaxConns, err := requiredInt32("DATABASE_MAX_CONNS")
	if err != nil {
		return DatabaseConfig{}, err
	}

	databaseMinConns, err := requiredInt32("DATABASE_MIN_CONNS")
	if err != nil {
		return DatabaseConfig{}, err
	}

	databaseMaxConnLifetime, err := requiredDuration("DATABASE_MAX_CONN_LIFETIME")
	if err != nil {
		return DatabaseConfig{}, err
	}

	databaseConnectTimeout, err := requiredDuration("DATABASE_CONNECT_TIMEOUT")
	if err != nil {
		return DatabaseConfig{}, err
	}

	databaseQueryTimeout, err := requiredDuration("DATABASE_QUERY_TIMEOUT")
	if err != nil {
		return DatabaseConfig{}, err
	}

	cfg := DatabaseConfig{
		URL:             databaseURL,
		MaxConns:        databaseMaxConns,
		MinConns:        databaseMinConns,
		MaxConnLifetime: databaseMaxConnLifetime,
		ConnectTimeout:  databaseConnectTimeout,
		QueryTimeout:    databaseQueryTimeout,
	}
	if err := cfg.validate(); err != nil {
		return DatabaseConfig{}, fmt.Errorf("validate database configuration: %w", err)
	}

	return cfg, nil
}

func (cfg DatabaseConfig) validate() error {
	if cfg.MaxConns <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONNS must be greater than zero")
	}

	if cfg.MinConns < 0 {
		return fmt.Errorf("DATABASE_MIN_CONNS must not be negative")
	}

	if cfg.MinConns > cfg.MaxConns {
		return fmt.Errorf("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}

	return nil
}

func validateTelemetry() error {
	for _, name := range []string{"OTEL_SERVICE_NAME", "OTEL_EXPORTER_OTLP_ENDPOINT"} {
		if _, err := requiredString(name); err != nil {
			return err
		}
	}

	protocol, err := requiredString("OTEL_EXPORTER_OTLP_PROTOCOL")
	if err != nil {
		return err
	}
	if protocol != "grpc" && protocol != "http/protobuf" {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_PROTOCOL must be grpc or http/protobuf")
	}

	sampler, err := requiredString("OTEL_TRACES_SAMPLER")
	if err != nil {
		return err
	}
	return validateSampler(sampler)
}

func validateSampler(sampler string) error {
	switch sampler {
	case "always_on", "always_off", "parentbased_always_on", "parentbased_always_off":
		return nil
	case "traceidratio", "parentbased_traceidratio":
		return validateSamplerRatio()
	default:
		return fmt.Errorf("OTEL_TRACES_SAMPLER has unsupported value %q", sampler)
	}
}

func validateSamplerRatio() error {
	value, ok := os.LookupEnv("OTEL_TRACES_SAMPLER_ARG")
	if !ok {
		return nil
	}
	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(ratio) || ratio < 0 || ratio > 1 {
		return fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be a number between 0 and 1")
	}
	return nil
}

func requiredString(name string) (string, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is empty", name)
	}

	return value, nil
}

func requiredDuration(name string) (time.Duration, error) {
	value, err := requiredString(name)
	if err != nil {
		return 0, err
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q as duration: %w", name, value, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return duration, nil
}

func requiredInt32(name string) (int32, error) {
	value, err := requiredString(name)
	if err != nil {
		return 0, err
	}

	number, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q as integer: %w", name, value, err)
	}

	return int32(number), nil
}

func requiredLogLevel(name string) (slog.Level, error) {
	value, err := requiredString(name)
	if err != nil {
		return 0, err
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return 0, fmt.Errorf("parse %s=%q as log level: %w", name, value, err)
	}

	return level, nil
}
