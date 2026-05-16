package model

import "time"

// LeaseSignature captures the signing event with enough detail
// to prove who signed, what they signed, when, and from where.
type LeaseSignature struct {
	ID            string    `json:"id"`
	LeaseID       string    `json:"lease_id"`
	TenantID      string    `json:"tenant_id"`
	DocumentHash  string    `json:"document_hash"`  // SHA-256 of lease document at time of signing
	IPAddress     string    `json:"ip_address"`
	UserAgent     string    `json:"user_agent"`
	DeviceID      string    `json:"device_id,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	ConsentText   string    `json:"consent_text"`   // Exact text the tenant agreed to
}
