package model

import "time"

// OutboxEvent represents a domain event stored in the outbox table.
// Uses the transactional outbox pattern: events are written in the same
// transaction as the state change, guaranteeing at-least-once delivery.
type OutboxEvent struct {
	ID          string    `json:"id"`
	AggregateID string    `json:"aggregate_id"` // e.g., lease ID
	EventType   string    `json:"event_type"`
	Payload     []byte    `json:"payload"`
	CreatedAt   time.Time `json:"created_at"`
	Published   bool      `json:"published"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// Event types
const (
	EventLeaseDepositInitiated = "lease.deposit.initiated"
	EventLeaseDepositPaid      = "lease.deposit.paid"
	EventLeaseDepositFailed    = "lease.deposit.failed"
	EventLeaseSigned           = "lease.signed"
	EventLeaseActivated        = "lease.activated"
	EventLeaseAbandoned        = "lease.abandoned"
)
