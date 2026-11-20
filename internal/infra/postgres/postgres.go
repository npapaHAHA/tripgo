package postgres

import (
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/npapaHAHA/tripgo/internal/config"
)

func PoolConfig(cfg config.DatabaseConfig, provider trace.TracerProvider, metrics queryMetrics) (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	poolConfig.ConnConfig.Tracer = &queryTracer{
		inner: otelpgx.NewTracer(
			otelpgx.WithTracerProvider(provider),
			otelpgx.WithDisableSQLStatementInAttributes(),
			otelpgx.WithDisableConnectionDetailsInAttributes(),
		),
		metrics: metrics,
	}
	return poolConfig, nil
}
