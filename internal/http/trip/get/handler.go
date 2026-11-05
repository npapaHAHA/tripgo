package get

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/http/domainerror"
	triphttp "github.com/npapaHAHA/tripgo/internal/http/trip"
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
	value, err := h.usecase.Execute(r.Context(), id)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}
	domainerror.WriteJSON(w, http.StatusOK, triphttp.ToAPI(value))
}
