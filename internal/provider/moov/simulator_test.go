package moov

import "testing"

func TestVerifyCallbackToken_Valid(t *testing.T) {
	if !VerifyCallbackToken("Bearer " + SimulatedBearerToken) {
		t.Error("expected valid token to pass verification")
	}
}

func TestVerifyCallbackToken_Invalid(t *testing.T) {
	cases := []string{
		"",
		"Bearer wrong-token",
		"Basic " + SimulatedBearerToken,
		SimulatedBearerToken, // missing "Bearer " prefix
	}
	for _, tc := range cases {
		if VerifyCallbackToken(tc) {
			t.Errorf("expected token %q to fail verification", tc)
		}
	}
}

func TestCallbackPayload_IsSuccessful(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{StatusCompleted, true},
		{StatusRejected, false},
		{StatusTimeout, false},
		{StatusInitiated, false},
	}
	for _, tt := range tests {
		cb := CallbackPayload{StatusCode: tt.status}
		if cb.IsSuccessful() != tt.want {
			t.Errorf("status %s: IsSuccessful() = %v, want %v", tt.status, cb.IsSuccessful(), tt.want)
		}
	}
}
