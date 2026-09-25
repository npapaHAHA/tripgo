package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"

	"github.com/npapaHAHA/tripgo/internal/config"
	"github.com/npapaHAHA/tripgo/internal/database"
	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/httpapi"
	"github.com/npapaHAHA/tripgo/internal/service"
)

const serviceName = "trip-service"

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})).With("service", serviceName)

	if err := run(cfg, logger); err != nil {
		logger.Error("service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	appCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(appCtx, cfg.Database)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	repository := database.NewTripRepository(pool)
	txManager := database.NewTxManager(pool)
	tripService := service.NewTripService(repository, txManager, cfg.Database.QueryTimeout)
	handler := httpapi.NewHandler(pool, tripService, cfg.Database.QueryTimeout, logger)
	router := chi.NewRouter()
	routes := api.HandlerWithOptions(handler, api.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: handler.HandleRequestError,
	})

	server := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           routes,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP server started", "address", cfg.HTTP.Addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-appCtx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}

	logger.Info("service stopped")

	return nil
}
