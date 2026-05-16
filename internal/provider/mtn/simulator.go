// Package mtn simulates the MTN MoMo Collections API.
//
// Real MTN MoMo flow:
// 1. Merchant POSTs to /collection/v1_0/requesttopay with amount, payer MSISDN, externalId
// 2. MTN responds 202 Accepted with a referenceId header
// 3. MTN processes the payment (user confirms on their phone)
// 4. MTN POSTs a callback to the merchant's configured URL with the outcome
//
// The callback includes an HMAC-SHA256 signature in the X-Callback-Signature header,
// computed over the raw body using a shared secret (API key).
package mtn

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"
)

const (
	// SimulatedAPIKey is the shared secret for HMAC callback authentication
	SimulatedAPIKey = "mtn-sandbox-api-key-2024"

	StatusSuccessful = "SUCCESSFUL"
	StatusFailed     = "FAILED"
	StatusPending    = "PENDING"
)

// CollectRequest mirrors the real MTN MoMo requesttopay body.
type CollectRequest struct {
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	ExternalID   string `json:"externalId"`
	Payer        Payer  `json:"payer"`
	PayerMessage string `json:"payerMessage"`
	PayeeNote    string `json:"payeeNote"`
}

type Payer struct {
	PartyIDType string `json:"partyIdType"` // MSISDN
	PartyID     string `json:"partyId"`     // Phone number
}

// CallbackPayload is what MTN sends back to us.
type CallbackPayload struct {
	ReferenceID    string `json:"referenceId"`
	ExternalID     string `json:"externalId"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	Payer          Payer  `json:"payer"`
	Status         string `json:"status"` // SUCCESSFUL or FAILED
	Reason         string `json:"reason,omitempty"`
	FinancialTxID  string `json:"financialTransactionId,omitempty"`
}

func (c CallbackPayload) IsSuccessful() bool {
	return c.Status == StatusSuccessful
}

func (c CallbackPayload) FailureReason() string {
	if c.Status == StatusFailed {
		if c.Reason != "" {
			return c.Reason
		}
		return "payment declined by payer"
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

// RequestCollection simulates sending a collection request to MTN.
// After a short delay, it fires a callback with the outcome.
func (s *Simulator) RequestCollection(ctx context.Context, externalID, phoneNumber string, amount int64) error {
	slog.Info("MTN simulator: collection request received",
		"external_id", externalID,
		"phone", phoneNumber,
		"amount", amount,
	)

	// Simulate async processing delay (1-3 seconds)
	go func() {
		delay := time.Duration(1000+rand.Intn(2000)) * time.Millisecond
		time.Sleep(delay)
		s.sendCallback(externalID, phoneNumber, amount)
	}()

	return nil
}

func (s *Simulator) sendCallback(externalID, phoneNumber string, amount int64) {
	// Simulate ~80% success rate
	success := rand.Float64() < 0.8

	callback := CallbackPayload{
		ReferenceID: fmt.Sprintf("mtn-%s", externalID[:8]),
		ExternalID:  externalID,
		Amount:      fmt.Sprintf("%d", amount),
		Currency:    "XOF",
		Payer: Payer{
			PartyIDType: "MSISDN",
			PartyID:     phoneNumber,
		},
	}

	if success {
		callback.Status = StatusSuccessful
		callback.FinancialTxID = fmt.Sprintf("FT%d", time.Now().UnixNano())
	} else {
		callback.Status = StatusFailed
		reasons := []string{
			"PAYER_NOT_FOUND",
			"NOT_ENOUGH_FUNDS",
			"PAYEE_NOT_ALLOWED_TO_RECEIVE",
			"TRANSACTION_REFUSED",
		}
		callback.Reason = reasons[rand.Intn(len(reasons))]
	}

	body, _ := json.Marshal(callback)

	// Sign the callback with HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(SimulatedAPIKey))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	url := s.callbackBaseURL + "/mtn"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("MTN simulator: failed to create callback request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Callback-Signature", signature)
	req.Header.Set("X-Reference-Id", callback.ReferenceID)

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Error("MTN simulator: failed to send callback", "error", err)
		return
	}
	defer resp.Body.Close()

	slog.Info("MTN simulator: callback sent",
		"external_id", externalID,
		"status", callback.Status,
		"http_status", resp.StatusCode,
	)
}

// HandleCollectRequest is the simulator's HTTP endpoint that mimics the real MTN API.
// The app's integration code calls this instead of the real MTN endpoint.
func (s *Simulator) HandleCollectRequest(w http.ResponseWriter, r *http.Request) {
	var req CollectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	amount := int64(0)
	fmt.Sscanf(req.Amount, "%d", &amount)

	go func() {
		delay := time.Duration(1000+rand.Intn(2000)) * time.Millisecond
		time.Sleep(delay)
		s.sendCallback(req.ExternalID, req.Payer.PartyID, amount)
	}()

	// MTN responds 202 Accepted immediately
	w.Header().Set("X-Reference-Id", fmt.Sprintf("mtn-%s", req.ExternalID[:8]))
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "PENDING"})
}

// VerifyCallbackSignature verifies the HMAC signature on an incoming MTN callback.
func VerifyCallbackSignature(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(SimulatedAPIKey))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
