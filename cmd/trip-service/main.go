package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/npapaHAHA/tripgo/internal/config"
	"github.com/npapaHAHA/tripgo/internal/container"
	"github.com/npapaHAHA/tripgo/internal/infra/telemetry"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		telemetry.NewLogger(slog.LevelInfo).Error("load configuration", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(cfg.LogLevel)
	if err := run(cfg, logger); err != nil {
		logger.Error("service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, logger *slog.Logger) (resultErr error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := container.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := app.Server.Shutdown(shutdownCtx); err != nil {
			_ = app.Server.Close()
			resultErr = errors.Join(resultErr, fmt.Errorf("shutdown HTTP server: %w", err))
		}
		resultErr = errors.Join(resultErr, app.Close(shutdownCtx))
		logger.Info("service stopped")
	}()
	listener, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Server.Serve(listener) }()
	logger.Info("HTTP server started", "address", listener.Addr().String())
	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}
	return nil
}
