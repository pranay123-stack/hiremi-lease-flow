package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/events"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/repository"
)

type LeaseService struct {
	leases    *repository.LeaseRepository
	payments  *repository.PaymentRepository
	publisher *events.PostgresOutbox
}

func NewLeaseService(
	leases *repository.LeaseRepository,
	payments *repository.PaymentRepository,
	publisher *events.PostgresOutbox,
) *LeaseService {
	return &LeaseService{
		leases:    leases,
		payments:  payments,
		publisher: publisher,
	}
}

func (s *LeaseService) GetLease(ctx context.Context, leaseID string) (*model.Lease, error) {
	return s.leases.GetByID(ctx, leaseID)
}

type SignLeaseRequest struct {
	TenantID  string
	IPAddress string
	UserAgent string
	DeviceID  string
}

func (s *LeaseService) SignLease(ctx context.Context, leaseID string, req SignLeaseRequest) error {
	tx, err := s.leases.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	lease, err := s.leases.GetByIDForUpdate(ctx, tx, leaseID)
	if err != nil {
		return err
	}

	if lease.Status != model.LeaseStatusDepositPaid {
		return model.ErrNotSignable
	}

	if lease.TenantID != req.TenantID {
		return fmt.Errorf("tenant %s does not own lease %s", req.TenantID, leaseID)
	}

	// Build a deterministic hash of the lease content for integrity proof
	docContent := fmt.Sprintf("lease:%s:tenant:%s:property:%s:rent:%d:deposit:%d",
		lease.ID, lease.TenantID, lease.PropertyID, lease.RentAmount, lease.DepositAmount)
	hash := sha256.Sum256([]byte(docContent))
	documentHash := hex.EncodeToString(hash[:])

	now := time.Now().UTC()
	consentText := fmt.Sprintf(
		"I, tenant %s, hereby sign the lease agreement for property %s. "+
			"I agree to pay monthly rent of %d XOF. This signature is legally binding.",
		lease.TenantID, lease.PropertyID, lease.RentAmount,
	)

	sig := &model.LeaseSignature{
		ID:           uuid.New().String(),
		LeaseID:      leaseID,
		TenantID:     req.TenantID,
		DocumentHash: documentHash,
		IPAddress:    req.IPAddress,
		UserAgent:    req.UserAgent,
		DeviceID:     req.DeviceID,
		Timestamp:    now,
		ConsentText:  consentText,
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO lease_signatures (id, lease_id, tenant_id, document_hash, ip_address, user_agent, device_id, timestamp, consent_text)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		sig.ID, sig.LeaseID, sig.TenantID, sig.DocumentHash, sig.IPAddress, sig.UserAgent, sig.DeviceID, sig.Timestamp, sig.ConsentText,
	)
	if err != nil {
		return fmt.Errorf("store signature: %w", err)
	}

	// Transition: deposit_paid -> signed
	if err := lease.TransitionTo(model.LeaseStatusSigned); err != nil {
		return err
	}
	lease.SignedAt = &now
	if err := s.leases.Update(ctx, tx, lease); err != nil {
		return err
	}

	if err := s.publisher.Publish(ctx, tx, leaseID, model.EventLeaseSigned, map[string]any{
		"lease_id":      leaseID,
		"tenant_id":     req.TenantID,
		"signed_at":     now,
		"document_hash": documentHash,
		"signature_id":  sig.ID,
	}); err != nil {
		return err
	}

	// Transition: signed -> active (immediate, same transaction)
	if err := lease.TransitionTo(model.LeaseStatusActive); err != nil {
		return err
	}
	lease.ActivatedAt = &now
	if err := s.leases.Update(ctx, tx, lease); err != nil {
		return err
	}

	// Create first month's rent schedule
	firstRent := &model.RentSchedule{
		ID:        uuid.New().String(),
		LeaseID:   leaseID,
		TenantID:  lease.TenantID,
		Amount:    lease.RentAmount,
		DueDate:   now.AddDate(0, 1, 0), // Due one month from activation
		Period:    1,
		CreatedAt: now,
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO rent_schedules (id, lease_id, tenant_id, amount, due_date, period, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		firstRent.ID, firstRent.LeaseID, firstRent.TenantID, firstRent.Amount,
		firstRent.DueDate, firstRent.Period, firstRent.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create rent schedule: %w", err)
	}

	if err := s.publisher.Publish(ctx, tx, leaseID, model.EventLeaseActivated, map[string]any{
		"lease_id":     leaseID,
		"tenant_id":    lease.TenantID,
		"activated_at": now,
		"first_rent": map[string]any{
			"amount":   firstRent.Amount,
			"due_date": firstRent.DueDate,
		},
	}); err != nil {
		return err
	}

	slog.Info("lease signed and activated",
		"lease_id", leaseID,
		"tenant_id", req.TenantID,
	)

	return tx.Commit(ctx)
}

// AbandonLease marks a lease as abandoned. Can happen from approved, deposit_pending, or deposit_paid states.
func (s *LeaseService) AbandonLease(ctx context.Context, leaseID, tenantID string) error {
	tx, err := s.leases.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	lease, err := s.leases.GetByIDForUpdate(ctx, tx, leaseID)
	if err != nil {
		return err
	}

	if lease.TenantID != tenantID {
		return fmt.Errorf("tenant %s does not own lease %s", tenantID, leaseID)
	}

	if err := lease.TransitionTo(model.LeaseStatusAbandoned); err != nil {
		return err
	}
	if err := s.leases.Update(ctx, tx, lease); err != nil {
		return err
	}

	if err := s.publisher.Publish(ctx, tx, leaseID, model.EventLeaseAbandoned, map[string]any{
		"lease_id":  leaseID,
		"tenant_id": tenantID,
		"from_state": lease.Status,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Helper to begin a transaction — exposed for payment service coordination
func (s *LeaseService) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return s.leases.BeginTx(ctx)
}
