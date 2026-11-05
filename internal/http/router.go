package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/http/domainerror"
)

type router struct {
	create           http.Handler
	get              http.Handler
	finish           http.Handler
	db               readinessChecker
	readinessTimeout time.Duration
	logger           *slog.Logger
}

func NewRouter(createHandler, getHandler, finishHandler http.Handler, db readinessChecker, timeout time.Duration, logger *slog.Logger) http.Handler {
	h := &router{
		create: createHandler, get: getHandler, finish: finishHandler,
		db: db, readinessTimeout: timeout, logger: logger,
	}
	mux := chi.NewRouter()
	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		domainerror.WriteStatus(w, r, http.StatusNotFound, "not_found")
	})
	mux.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		domainerror.WriteStatus(w, r, http.StatusMethodNotAllowed, "method_not_allowed")
	})
	return api.HandlerWithOptions(h, api.ChiServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			domainerror.Write(w, r, &trip.ValidationError{Field: "request", Message: "parameters are invalid"})
		},
	})
}

func (h *router) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	h.create.ServeHTTP(w, r)
}

func (h *router) GetTrip(w http.ResponseWriter, r *http.Request, _ api.TripId) {
	h.get.ServeHTTP(w, r)
}

func (h *router) FinishTrip(w http.ResponseWriter, r *http.Request, _ api.TripId) {
	h.finish.ServeHTTP(w, r)
}

func (h *router) Health(w http.ResponseWriter, _ *http.Request) {
	domainerror.WriteJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (h *router) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.readinessTimeout)
	defer cancel()
	if h.db == nil {
		domainerror.WriteJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	if err := h.db.Ping(ctx); err != nil {
		h.logger.ErrorContext(ctx, "database readiness failed", "error", err)
		domainerror.WriteJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	domainerror.WriteJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
