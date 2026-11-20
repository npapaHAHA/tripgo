package list

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	api "github.com/npapaHAHA/tripgo/internal/generated"
	"github.com/npapaHAHA/tripgo/internal/http/domainerror"
	positionhttp "github.com/npapaHAHA/tripgo/internal/http/position"
)

type Handler struct {
	usecase usecase
}

func New(uc usecase) *Handler {
	return &Handler{usecase: uc}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "tripId"))
	if err != nil {
		domainerror.Write(w, r, &trip.ValidationError{Field: "tripId", Message: "must be a UUID"})
		return
	}
	values, err := h.usecase.Execute(r.Context(), id)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}
	positions := make([]api.TripPosition, 0, len(values))
	for _, value := range values {
		positions = append(positions, positionhttp.ToAPI(value))
	}
	domainerror.WriteJSON(w, http.StatusOK, api.TripPositionsResponse{Positions: positions})
}
