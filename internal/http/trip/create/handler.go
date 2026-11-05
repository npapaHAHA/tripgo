package create

import (
	"net/http"

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
	input, err := decode(r)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}
	value, err := h.usecase.Execute(r.Context(), input)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/trips/"+value.ID.String())
	domainerror.WriteJSON(w, http.StatusCreated, triphttp.ToAPI(value))
}
