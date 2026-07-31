package httpapi

import (
	"net/http"

	"iterpugov/go-payment-ledger/ledger"
)

type Handler struct {
	svc *ledger.Service
}

func NewHandler(svc *ledger.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	panic("TODO: CreateAccount")
}

func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	panic("TODO: GetAccount")
}

func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	panic("TODO: CreateTransfer — Idempotency-Key header")
}

func (h *Handler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	panic("TODO: GetTransfer")
}

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	panic("TODO: Healthz — ping DB")
}
