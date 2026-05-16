package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/moov"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/mtn"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/service"
)

type CallbackHandler struct {
	svc *service.PaymentService
}

func NewCallbackHandler(svc *service.PaymentService) *CallbackHandler {
	return &CallbackHandler{svc: svc}
}

// HandleMTNCallback receives and verifies callbacks from MTN MoMo.
// Authentication: HMAC-SHA256 signature in X-Callback-Signature header.
func (h *CallbackHandler) HandleMTNCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// Verify HMAC signature
	signature := r.Header.Get("X-Callback-Signature")
	if signature == "" {
		slog.Warn("MTN callback missing signature header")
		writeError(w, http.StatusUnauthorized, "missing callback signature")
		return
	}

	if !mtn.VerifyCallbackSignature(body, signature) {
		slog.Warn("MTN callback signature verification failed")
		writeError(w, http.StatusUnauthorized, "invalid callback signature")
		return
	}

	var callback mtn.CallbackPayload
	if err := json.Unmarshal(body, &callback); err != nil {
		writeError(w, http.StatusBadRequest, "invalid callback payload")
		return
	}

	if err := h.svc.HandleMTNCallback(r.Context(), callback); err != nil {
		slog.Error("failed to process MTN callback", "error", err)
		// Return 200 to prevent provider from retrying (we logged it, we'll handle manually)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// HandleMoovCallback receives and verifies callbacks from Moov Money.
// Authentication: Bearer token in Authorization header.
func (h *CallbackHandler) HandleMoovCallback(w http.ResponseWriter, r *http.Request) {
	// Verify bearer token
	authHeader := r.Header.Get("Authorization")
	if !moov.VerifyCallbackToken(authHeader) {
		slog.Warn("Moov callback authentication failed")
		writeError(w, http.StatusUnauthorized, "invalid authorization")
		return
	}

	var callback moov.CallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&callback); err != nil {
		writeError(w, http.StatusBadRequest, "invalid callback payload")
		return
	}

	if err := h.svc.HandleMoovCallback(r.Context(), callback); err != nil {
		slog.Error("failed to process Moov callback", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}
