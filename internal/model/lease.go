package model

import (
	"errors"
	"time"
)

// Lease states — strict progression, no skipping.
type LeaseStatus string

const (
	LeaseStatusApproved       LeaseStatus = "approved"        // Awaiting deposit payment
	LeaseStatusDepositPending LeaseStatus = "deposit_pending" // Payment initiated, waiting for callback
	LeaseStatusDepositPaid    LeaseStatus = "deposit_paid"    // Deposit confirmed, ready to sign
	LeaseStatusSigned         LeaseStatus = "signed"          // Tenant has signed
	LeaseStatusActive         LeaseStatus = "active"          // Lease is active, rent schedule created
	LeaseStatusAbandoned      LeaseStatus = "abandoned"       // Tenant abandoned the flow
)

var validTransitions = map[LeaseStatus][]LeaseStatus{
	LeaseStatusApproved:       {LeaseStatusDepositPending, LeaseStatusAbandoned},
	LeaseStatusDepositPending: {LeaseStatusDepositPaid, LeaseStatusApproved, LeaseStatusAbandoned}, // Back to approved on failure (retry)
	LeaseStatusDepositPaid:    {LeaseStatusSigned, LeaseStatusAbandoned},
	LeaseStatusSigned:         {LeaseStatusActive},
}

var (
	ErrInvalidTransition = errors.New("invalid lease state transition")
	ErrLeaseNotFound     = errors.New("lease not found")
	ErrPaymentNotFound   = errors.New("payment not found")
	ErrAlreadySigned     = errors.New("lease already signed")
	ErrNotSignable       = errors.New("lease is not in a signable state")
	ErrDepositExists     = errors.New("a pending deposit payment already exists")
)

type Lease struct {
	ID          string      `json:"id"`
	TenantID    string      `json:"tenant_id"`
	PropertyID  string      `json:"property_id"`
	RentAmount  int64       `json:"rent_amount"`  // XOF, integer
	DepositAmount int64     `json:"deposit_amount"` // XOF, integer
	Status      LeaseStatus `json:"status"`
	SignedAt    *time.Time  `json:"signed_at,omitempty"`
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Version     int         `json:"version"` // Optimistic locking
}

func (l *Lease) CanTransitionTo(target LeaseStatus) bool {
	allowed, ok := validTransitions[l.Status]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == target {
			return true
		}
	}
	return false
}

func (l *Lease) TransitionTo(target LeaseStatus) error {
	if !l.CanTransitionTo(target) {
		return ErrInvalidTransition
	}
	l.Status = target
	l.UpdatedAt = time.Now().UTC()
	return nil
}
