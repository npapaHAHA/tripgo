package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/service"
)

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	db               ReadinessChecker
	tripService      *service.TripService
	readinessTimeout time.Duration
	logger           *slog.Logger
}

var _ api.ServerInterface = (*Handler)(nil)

func NewHandler(
	db ReadinessChecker,
	tripService *service.TripService,
	readinessTimeout time.Duration,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		db:               db,
		tripService:      tripService,
		readinessTimeout: readinessTimeout,
		logger:           logger,
	}
}

// Health reports that the process is running. It deliberately does not access
// the database or any other external dependency.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.readinessTimeout)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}

	h.writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
