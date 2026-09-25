// Package config loads and validates the service configuration.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains all runtime settings required by the service.
type Config struct {
	HTTPAddr        string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	Database        DatabaseConfig
}

// DatabaseConfig contains the PostgreSQL connection and pool settings.
type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
}

// Load reads configuration from environment variables and validates it.
func Load() (Config, error) {
	httpAddr, err := requiredString("HTTP_ADDR")
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

	databaseURL, err := requiredString("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	databaseMaxConns, err := requiredInt32("DATABASE_MAX_CONNS")
	if err != nil {
		return Config{}, err
	}

	databaseMinConns, err := requiredInt32("DATABASE_MIN_CONNS")
	if err != nil {
		return Config{}, err
	}

	databaseMaxConnLifetime, err := requiredDuration("DATABASE_MAX_CONN_LIFETIME")
	if err != nil {
		return Config{}, err
	}

	databaseConnectTimeout, err := requiredDuration("DATABASE_CONNECT_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	databaseQueryTimeout, err := requiredDuration("DATABASE_QUERY_TIMEOUT")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:        httpAddr,
		LogLevel:        logLevel,
		ShutdownTimeout: shutdownTimeout,
		Database: DatabaseConfig{
			URL:             databaseURL,
			MaxConns:        databaseMaxConns,
			MinConns:        databaseMinConns,
			MaxConnLifetime: databaseMaxConnLifetime,
			ConnectTimeout:  databaseConnectTimeout,
			QueryTimeout:    databaseQueryTimeout,
		},
	}

	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("validate configuration: %w", err)
	}

	return cfg, nil
}

func (cfg Config) validate() error {
	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be greater than zero")
	}

	if cfg.Database.MaxConns <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONNS must be greater than zero")
	}

	if cfg.Database.MinConns < 0 {
		return fmt.Errorf("DATABASE_MIN_CONNS must not be negative")
	}

	if cfg.Database.MinConns > cfg.Database.MaxConns {
		return fmt.Errorf("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}

	if cfg.Database.MaxConnLifetime <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONN_LIFETIME must be greater than zero")
	}

	if cfg.Database.ConnectTimeout <= 0 {
		return fmt.Errorf("DATABASE_CONNECT_TIMEOUT must be greater than zero")
	}

	if cfg.Database.QueryTimeout <= 0 {
		return fmt.Errorf("DATABASE_QUERY_TIMEOUT must be greater than zero")
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
