package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/service"
)

type PaymentHandler struct {
	svc *service.PaymentService
}

func NewPaymentHandler(svc *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

type initiateDepositRequest struct {
	TenantID    string `json:"tenant_id"`
	Provider    string `json:"provider"`     // "mtn_momo" or "moov_money"
	PhoneNumber string `json:"phone_number"`
}

func (h *PaymentHandler) InitiateDeposit(w http.ResponseWriter, r *http.Request) {
	leaseID := chi.URLParam(r, "leaseID")

	var req initiateDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TenantID == "" || req.Provider == "" || req.PhoneNumber == "" {
		writeError(w, r, http.StatusBadRequest, "tenant_id, provider, and phone_number are required")
		return
	}

	provider := model.PaymentProvider(req.Provider)
	if provider != model.ProviderMTN && provider != model.ProviderMoov {
		writeError(w, r, http.StatusBadRequest, "provider must be 'mtn_momo' or 'moov_money'")
		return
	}

	payment, err := h.svc.InitiateDeposit(r.Context(), leaseID, service.InitiateDepositRequest{
		TenantID:    req.TenantID,
		Provider:    provider,
		PhoneNumber: req.PhoneNumber,
	})
	if err != nil {
		if errors.Is(err, model.ErrLeaseNotFound) {
			writeError(w, r, http.StatusNotFound, "lease not found")
			return
		}
		if errors.Is(err, model.ErrDepositExists) {
			writeError(w, r, http.StatusConflict, "a pending deposit payment already exists for this lease")
			return
		}
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"payment_id": payment.ID,
		"status":     payment.Status,
		"provider":   payment.Provider,
		"amount":     payment.Amount,
		"message":    "deposit payment initiated, awaiting provider confirmation",
	})
}

func (h *PaymentHandler) GetDepositStatus(w http.ResponseWriter, r *http.Request) {
	leaseID := chi.URLParam(r, "leaseID")

	payment, err := h.svc.GetDepositStatus(r.Context(), leaseID)
	if err != nil {
		if errors.Is(err, model.ErrPaymentNotFound) {
			writeError(w, r, http.StatusNotFound, "no deposit payment found for this lease")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, payment)
}
