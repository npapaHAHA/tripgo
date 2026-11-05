package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/npapaHAHA/tripgo/internal/config"
	httpapi "github.com/npapaHAHA/tripgo/internal/http"
	"github.com/npapaHAHA/tripgo/internal/http/middleware"
	createhandler "github.com/npapaHAHA/tripgo/internal/http/trip/create"
	finishhandler "github.com/npapaHAHA/tripgo/internal/http/trip/finish"
	gethandler "github.com/npapaHAHA/tripgo/internal/http/trip/get"
	"github.com/npapaHAHA/tripgo/internal/infra/postgres"
	"github.com/npapaHAHA/tripgo/internal/infra/postgres/transaction"
	"github.com/npapaHAHA/tripgo/internal/infra/telemetry"
	triprepo "github.com/npapaHAHA/tripgo/internal/repository/trip"
	"github.com/npapaHAHA/tripgo/internal/usecase/trip/create"
	"github.com/npapaHAHA/tripgo/internal/usecase/trip/finish"
	"github.com/npapaHAHA/tripgo/internal/usecase/trip/get"
)

type Container struct {
	Server    *http.Server
	pool      *pgxpool.Pool
	telemetry *telemetry.Telemetry
	logger    *slog.Logger
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (_ *Container, resultErr error) {
	tel, err := telemetry.Setup(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}
	app := &Container{telemetry: tel, logger: logger}
	defer func() {
		if resultErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
			defer cancel()
			resultErr = errors.Join(resultErr, app.Close(cleanupCtx))
		}
	}()

	poolCfg, err := postgres.PoolConfig(cfg.Database, tel.TracerProvider, tel)
	if err != nil {
		return nil, err
	}
	connectCtx, cancel := context.WithTimeout(ctx, cfg.Database.ConnectTimeout)
	defer cancel()
	app.pool, err = pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := app.pool.Ping(connectCtx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	repository := triprepo.NewTripRepository(app.pool)
	tx := transaction.New(app.pool)
	if err := tel.RegisterActiveTrips(func(ctx context.Context) (int64, error) {
		queryCtx, cancel := context.WithTimeout(ctx, cfg.Database.QueryTimeout)
		defer cancel()
		return repository.CountActive(queryCtx)
	}); err != nil {
		return nil, fmt.Errorf("register active trips metric: %w", err)
	}

	createUseCase := create.New(repository, tx, cfg.Database.QueryTimeout, logger, tel)
	getUseCase := get.New(repository, cfg.Database.QueryTimeout, logger)
	finishUseCase := finish.New(repository, tx, cfg.Database.QueryTimeout, logger, tel)
	router := httpapi.NewRouter(
		createhandler.New(createUseCase), gethandler.New(getUseCase), finishhandler.New(finishUseCase),
		app.pool, cfg.Database.QueryTimeout, logger,
	)
	handler, err := middleware.Wrap(router, logger, tel.TracerProvider, tel.Meter)
	if err != nil {
		return nil, fmt.Errorf("setup HTTP middleware: %w", err)
	}
	app.Server = &http.Server{
		Addr: cfg.HTTP.Addr, Handler: handler,
		ReadTimeout: cfg.HTTP.ReadTimeout, ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout,
	}
	return app, nil
}

func (c *Container) Close(ctx context.Context) error {
	err := c.telemetry.Shutdown(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "flush telemetry failed", "error", err)
	}
	err = ctx.Err()
	if c.pool != nil {
		closed := make(chan struct{})
		go func() {
			c.pool.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-ctx.Done():
			return errors.Join(err, fmt.Errorf("close database pool: %w", ctx.Err()))
		}
	}
	return err
}
