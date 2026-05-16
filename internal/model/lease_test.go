package model

import (
	"testing"
)

func TestLeaseStateTransitions(t *testing.T) {
	tests := []struct {
		name     string
		from     LeaseStatus
		to       LeaseStatus
		wantErr  bool
	}{
		{"approved to deposit_pending", LeaseStatusApproved, LeaseStatusDepositPending, false},
		{"approved to abandoned", LeaseStatusApproved, LeaseStatusAbandoned, false},
		{"approved to deposit_paid (skip)", LeaseStatusApproved, LeaseStatusDepositPaid, true},
		{"approved to signed (skip)", LeaseStatusApproved, LeaseStatusSigned, true},
		{"approved to active (skip)", LeaseStatusApproved, LeaseStatusActive, true},
		{"deposit_pending to deposit_paid", LeaseStatusDepositPending, LeaseStatusDepositPaid, false},
		{"deposit_pending to approved (retry)", LeaseStatusDepositPending, LeaseStatusApproved, false},
		{"deposit_pending to abandoned", LeaseStatusDepositPending, LeaseStatusAbandoned, false},
		{"deposit_pending to signed (skip)", LeaseStatusDepositPending, LeaseStatusSigned, true},
		{"deposit_paid to signed", LeaseStatusDepositPaid, LeaseStatusSigned, false},
		{"deposit_paid to abandoned", LeaseStatusDepositPaid, LeaseStatusAbandoned, false},
		{"deposit_paid to active (skip)", LeaseStatusDepositPaid, LeaseStatusActive, true},
		{"signed to active", LeaseStatusSigned, LeaseStatusActive, false},
		{"signed to abandoned (cannot)", LeaseStatusSigned, LeaseStatusAbandoned, true},
		{"active to anything", LeaseStatusActive, LeaseStatusApproved, true},
		{"active to abandoned", LeaseStatusActive, LeaseStatusAbandoned, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lease := &Lease{Status: tt.from}
			err := lease.TransitionTo(tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("TransitionTo(%s -> %s) error = %v, wantErr %v", tt.from, tt.to, err, tt.wantErr)
			}
			if err == nil && lease.Status != tt.to {
				t.Errorf("expected status %s, got %s", tt.to, lease.Status)
			}
		})
	}
}

func TestPaymentIsTerminal(t *testing.T) {
	tests := []struct {
		status   PaymentStatus
		terminal bool
	}{
		{PaymentStatusPending, false},
		{PaymentStatusSuccess, true},
		{PaymentStatusFailed, true},
		{PaymentStatusExpired, true},
	}
	for _, tt := range tests {
		p := &Payment{Status: tt.status}
		if p.IsTerminal() != tt.terminal {
			t.Errorf("Payment status %s: IsTerminal() = %v, want %v", tt.status, p.IsTerminal(), tt.terminal)
		}
	}
}
