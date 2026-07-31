package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(Recover)
	r.Use(RequestLog)

	r.Get("/healthz", h.Healthz)
	r.Post("/accounts", h.CreateAccount)
	r.Get("/accounts/{id}", h.GetAccount)
	r.Post("/transfers", h.CreateTransfer)
	r.Get("/transfers/{id}", h.GetTransfer)

	return r
}
