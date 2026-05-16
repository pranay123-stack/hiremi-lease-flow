package model

import "time"

type PaymentStatus string

const (
	PaymentStatusPending  PaymentStatus = "pending"
	PaymentStatusSuccess  PaymentStatus = "success"
	PaymentStatusFailed   PaymentStatus = "failed"
	PaymentStatusExpired  PaymentStatus = "expired"
)

type PaymentProvider string

const (
	ProviderMTN  PaymentProvider = "mtn_momo"
	ProviderMoov PaymentProvider = "moov_money"
)

type Payment struct {
	ID              string          `json:"id"`
	LeaseID         string          `json:"lease_id"`
	TenantID        string          `json:"tenant_id"`
	Amount          int64           `json:"amount"`       // XOF
	Provider        PaymentProvider `json:"provider"`
	ProviderTxID    string          `json:"provider_tx_id,omitempty"`
	PhoneNumber     string          `json:"phone_number"`
	Status          PaymentStatus   `json:"status"`
	FailureReason   string          `json:"failure_reason,omitempty"`
	CallbackPayload []byte          `json:"callback_payload,omitempty"` // Raw callback for audit
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Version         int             `json:"version"`
}

func (p *Payment) IsTerminal() bool {
	return p.Status == PaymentStatusSuccess || p.Status == PaymentStatusFailed || p.Status == PaymentStatusExpired
}
