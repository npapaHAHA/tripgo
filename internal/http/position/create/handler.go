package create

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
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
	input, err := decode(r)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}
	input.TripID = id
	value, err := h.usecase.Execute(r.Context(), input)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}
	domainerror.WriteJSON(w, http.StatusCreated, positionhttp.ToAPI(value))
}
