package model

import "time"

type RentSchedule struct {
	ID        string    `json:"id"`
	LeaseID   string    `json:"lease_id"`
	TenantID  string    `json:"tenant_id"`
	Amount    int64     `json:"amount"` // XOF
	DueDate   time.Time `json:"due_date"`
	Period    int       `json:"period"` // Month number (1 = first month)
	CreatedAt time.Time `json:"created_at"`
}
