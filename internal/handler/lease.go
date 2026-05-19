package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/service"
)

type LeaseHandler struct {
	svc *service.LeaseService
}

func NewLeaseHandler(svc *service.LeaseService) *LeaseHandler {
	return &LeaseHandler{svc: svc}
}

func (h *LeaseHandler) GetLease(w http.ResponseWriter, r *http.Request) {
	leaseID := chi.URLParam(r, "leaseID")

	lease, err := h.svc.GetLease(r.Context(), leaseID)
	if err != nil {
		if errors.Is(err, model.ErrLeaseNotFound) {
			writeError(w, r, http.StatusNotFound, "lease not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, lease)
}

type signRequest struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id,omitempty"`
}

func (h *LeaseHandler) SignLease(w http.ResponseWriter, r *http.Request) {
	leaseID := chi.URLParam(r, "leaseID")

	var req signRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TenantID == "" {
		writeError(w, r, http.StatusBadRequest, "tenant_id is required")
		return
	}

	err := h.svc.SignLease(r.Context(), leaseID, service.SignLeaseRequest{
		TenantID:  req.TenantID,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		DeviceID:  req.DeviceID,
	})
	if err != nil {
		if errors.Is(err, model.ErrNotSignable) {
			writeError(w, r, http.StatusConflict, "lease is not in a signable state (deposit must be paid first)")
			return
		}
		if errors.Is(err, model.ErrLeaseNotFound) {
			writeError(w, r, http.StatusNotFound, "lease not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "lease signed and activated",
	})
}

type abandonRequest struct {
	TenantID string `json:"tenant_id"`
}

func (h *LeaseHandler) AbandonLease(w http.ResponseWriter, r *http.Request) {
	leaseID := chi.URLParam(r, "leaseID")

	var req abandonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TenantID == "" {
		writeError(w, r, http.StatusBadRequest, "tenant_id is required")
		return
	}

	err := h.svc.AbandonLease(r.Context(), leaseID, req.TenantID)
	if err != nil {
		if errors.Is(err, model.ErrLeaseNotFound) {
			writeError(w, r, http.StatusNotFound, "lease not found")
			return
		}
		if errors.Is(err, model.ErrInvalidTransition) {
			writeError(w, r, http.StatusConflict, "lease cannot be abandoned from its current state")
			return
		}
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "lease abandoned",
	})
}
