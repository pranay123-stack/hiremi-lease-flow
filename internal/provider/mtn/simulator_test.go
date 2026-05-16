package mtn

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyCallbackSignature_Valid(t *testing.T) {
	body := []byte(`{"externalId":"test-123","status":"SUCCESSFUL"}`)

	mac := hmac.New(sha256.New, []byte(SimulatedAPIKey))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	if !VerifyCallbackSignature(body, signature) {
		t.Error("expected valid signature to pass verification")
	}
}

func TestVerifyCallbackSignature_Invalid(t *testing.T) {
	body := []byte(`{"externalId":"test-123","status":"SUCCESSFUL"}`)

	if VerifyCallbackSignature(body, "invalid-signature") {
		t.Error("expected invalid signature to fail verification")
	}
}

func TestVerifyCallbackSignature_TamperedBody(t *testing.T) {
	originalBody := []byte(`{"externalId":"test-123","status":"SUCCESSFUL"}`)
	tamperedBody := []byte(`{"externalId":"test-123","status":"FAILED"}`)

	mac := hmac.New(sha256.New, []byte(SimulatedAPIKey))
	mac.Write(originalBody)
	signature := hex.EncodeToString(mac.Sum(nil))

	if VerifyCallbackSignature(tamperedBody, signature) {
		t.Error("expected tampered body to fail verification")
	}
}

func TestCallbackPayload_IsSuccessful(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{StatusSuccessful, true},
		{StatusFailed, false},
		{StatusPending, false},
	}
	for _, tt := range tests {
		cb := CallbackPayload{Status: tt.status}
		if cb.IsSuccessful() != tt.want {
			t.Errorf("status %s: IsSuccessful() = %v, want %v", tt.status, cb.IsSuccessful(), tt.want)
		}
	}
}
