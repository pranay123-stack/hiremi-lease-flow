// Package moov simulates the Moov Money Payment API.
//
// Real Moov Money flow:
// 1. Merchant POSTs to /api/v1/payment/collect with reference, phone, amount
// 2. Moov responds 200 OK with transaction_id and status "INITIATED"
// 3. Moov processes (user confirms via USSD)
// 4. Moov POSTs callback with outcome, authenticated via a bearer token in Authorization header
//
// Key differences from MTN MoMo:
// - Uses bearer token auth on callbacks (not HMAC)
// - Different status vocabulary: "COMPLETED" vs MTN's "SUCCESSFUL"
// - Different field names: "reference" vs "externalId", "status_code" vs "status"
// - Returns transaction_id in initial response
package moov

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"
)

const (
	// SimulatedBearerToken authenticates callbacks from Moov to our platform
	SimulatedBearerToken = "moov-callback-bearer-token-2024"

	StatusCompleted = "COMPLETED"
	StatusRejected  = "REJECTED"
	StatusTimeout   = "TIMEOUT"
	StatusInitiated = "INITIATED"
)

// PaymentRequest mirrors Moov's collection API request.
type PaymentRequest struct {
	Reference   string `json:"reference"`
	PhoneNumber string `json:"phone_number"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Description string `json:"description,omitempty"`
}

// PaymentResponse is what Moov returns immediately.
type PaymentResponse struct {
	TransactionID string `json:"transaction_id"`
	Reference     string `json:"reference"`
	Status        string `json:"status"`
}

// CallbackPayload is what Moov sends to our callback URL.
type CallbackPayload struct {
	TransactionID string `json:"transaction_id"`
	Reference     string `json:"reference"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	PhoneNumber   string `json:"phone_number"`
	StatusCode    string `json:"status_code"` // COMPLETED, REJECTED, TIMEOUT
	Message       string `json:"message,omitempty"`
	CompletedAt   string `json:"completed_at,omitempty"`
}

func (c CallbackPayload) IsSuccessful() bool {
	return c.StatusCode == StatusCompleted
}

func (c CallbackPayload) FailureMessage() string {
	if c.StatusCode == StatusRejected || c.StatusCode == StatusTimeout {
		if c.Message != "" {
			return c.Message
		}
		if c.StatusCode == StatusTimeout {
			return "transaction timed out waiting for user confirmation"
		}
		return "payment rejected by user"
	}
	return ""
}

type Simulator struct {
	callbackBaseURL string
	client          *http.Client
}

func NewSimulator(callbackBaseURL string) *Simulator {
	return &Simulator{
		callbackBaseURL: callbackBaseURL,
		client:          &http.Client{Timeout: 10 * time.Second},
	}
}

// RequestPayment simulates sending a payment collection request to Moov.
func (s *Simulator) RequestPayment(ctx context.Context, reference, phoneNumber string, amount int64) error {
	slog.Info("Moov simulator: payment request received",
		"reference", reference,
		"phone", phoneNumber,
		"amount", amount,
	)

	go func() {
		// Moov is typically slightly slower than MTN (2-5 seconds)
		delay := time.Duration(2000+rand.Intn(3000)) * time.Millisecond
		time.Sleep(delay)
		s.sendCallback(reference, phoneNumber, amount)
	}()

	return nil
}

func (s *Simulator) sendCallback(reference, phoneNumber string, amount int64) {
	// Simulate ~75% success rate (Moov slightly lower than MTN in practice)
	roll := rand.Float64()

	callback := CallbackPayload{
		TransactionID: fmt.Sprintf("MOOV-%d", time.Now().UnixNano()),
		Reference:     reference,
		Amount:        amount,
		Currency:      "XOF",
		PhoneNumber:   phoneNumber,
	}

	if roll < 0.75 {
		callback.StatusCode = StatusCompleted
		callback.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	} else if roll < 0.90 {
		callback.StatusCode = StatusRejected
		messages := []string{
			"insufficient balance",
			"user rejected transaction",
			"account suspended",
		}
		callback.Message = messages[rand.Intn(len(messages))]
	} else {
		callback.StatusCode = StatusTimeout
		callback.Message = "no response from subscriber within timeout period"
	}

	body, _ := json.Marshal(callback)

	url := s.callbackBaseURL + "/moov"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("Moov simulator: failed to create callback request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+SimulatedBearerToken)
	req.Header.Set("X-Transaction-Id", callback.TransactionID)

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Error("Moov simulator: failed to send callback", "error", err)
		return
	}
	defer resp.Body.Close()

	slog.Info("Moov simulator: callback sent",
		"reference", reference,
		"status", callback.StatusCode,
		"http_status", resp.StatusCode,
	)
}

// HandlePaymentRequest is the simulator endpoint mimicking Moov's real API.
func (s *Simulator) HandlePaymentRequest(w http.ResponseWriter, r *http.Request) {
	var req PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	txID := fmt.Sprintf("MOOV-%d", time.Now().UnixNano())

	go func() {
		delay := time.Duration(2000+rand.Intn(3000)) * time.Millisecond
		time.Sleep(delay)
		s.sendCallback(req.Reference, req.PhoneNumber, req.Amount)
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(PaymentResponse{
		TransactionID: txID,
		Reference:     req.Reference,
		Status:        StatusInitiated,
	})
}

// VerifyCallbackToken checks the bearer token on incoming Moov callbacks.
func VerifyCallbackToken(authHeader string) bool {
	return authHeader == "Bearer "+SimulatedBearerToken
}
