package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/events"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/moov"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/provider/mtn"
	"github.com/pranay123-stack/hiremi-lease-flow/internal/repository"
)

type PaymentService struct {
	payments  *repository.PaymentRepository
	leases    *repository.LeaseRepository
	publisher *events.PostgresOutbox
	mtn       *mtn.Simulator
	moov      *moov.Simulator
}

func NewPaymentService(
	payments *repository.PaymentRepository,
	leases *repository.LeaseRepository,
	publisher *events.PostgresOutbox,
	mtnProvider *mtn.Simulator,
	moovProvider *moov.Simulator,
) *PaymentService {
	return &PaymentService{
		payments:  payments,
		leases:    leases,
		publisher: publisher,
		mtn:       mtnProvider,
		moov:      moovProvider,
	}
}

type InitiateDepositRequest struct {
	TenantID    string
	Provider    model.PaymentProvider
	PhoneNumber string
}

func (s *PaymentService) InitiateDeposit(ctx context.Context, leaseID string, req InitiateDepositRequest) (*model.Payment, error) {
	tx, err := s.leases.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	lease, err := s.leases.GetByIDForUpdate(ctx, tx, leaseID)
	if err != nil {
		return nil, err
	}

	if lease.TenantID != req.TenantID {
		return nil, fmt.Errorf("tenant %s does not own lease %s", req.TenantID, leaseID)
	}

	// Allow initiating deposit from approved state, or re-trying after a failed payment
	if lease.Status != model.LeaseStatusApproved && lease.Status != model.LeaseStatusDepositPending {
		return nil, fmt.Errorf("cannot initiate deposit: lease is in state %s", lease.Status)
	}

	// Check for existing pending payment
	existing, err := s.payments.GetPendingByLeaseID(ctx, leaseID)
	if err == nil && existing != nil {
		return nil, model.ErrDepositExists
	}

	// Create payment record
	now := time.Now().UTC()
	providerTxID := uuid.New().String()

	payment := &model.Payment{
		ID:           uuid.New().String(),
		LeaseID:      leaseID,
		TenantID:     req.TenantID,
		Amount:       lease.DepositAmount,
		Provider:     req.Provider,
		ProviderTxID: providerTxID,
		PhoneNumber:  req.PhoneNumber,
		Status:       model.PaymentStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
		Version:      0,
	}

	if err := s.payments.Create(ctx, tx, payment); err != nil {
		return nil, fmt.Errorf("create payment: %w", err)
	}

	// Transition lease to deposit_pending
	if lease.Status == model.LeaseStatusApproved {
		if err := lease.TransitionTo(model.LeaseStatusDepositPending); err != nil {
			return nil, err
		}
		if err := s.leases.Update(ctx, tx, lease); err != nil {
			return nil, err
		}
	}

	if err := s.publisher.Publish(ctx, tx, leaseID, model.EventLeaseDepositInitiated, map[string]any{
		"lease_id":       leaseID,
		"payment_id":     payment.ID,
		"provider":       req.Provider,
		"amount":         lease.DepositAmount,
		"phone_number":   req.PhoneNumber,
		"provider_tx_id": providerTxID,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Send collection request to provider (after commit — fire and forget)
	go s.sendToProvider(context.Background(), payment)

	return payment, nil
}

func (s *PaymentService) sendToProvider(ctx context.Context, payment *model.Payment) {
	var err error
	switch payment.Provider {
	case model.ProviderMTN:
		err = s.mtn.RequestCollection(ctx, payment.ProviderTxID, payment.PhoneNumber, payment.Amount)
	case model.ProviderMoov:
		err = s.moov.RequestPayment(ctx, payment.ProviderTxID, payment.PhoneNumber, payment.Amount)
	}
	if err != nil {
		slog.Error("failed to send collection request to provider",
			"provider", payment.Provider,
			"payment_id", payment.ID,
			"error", err,
		)
	}
}

// HandleMTNCallback processes an incoming MTN MoMo callback.
// Idempotent: if the payment is already in a terminal state, the callback is ignored.
func (s *PaymentService) HandleMTNCallback(ctx context.Context, callback mtn.CallbackPayload) error {
	slog.Info("received MTN callback",
		"external_id", callback.ExternalID,
		"status", callback.Status,
	)

	payment, err := s.payments.GetByProviderTxID(ctx, callback.ExternalID)
	if err != nil {
		return fmt.Errorf("payment not found for external_id %s: %w", callback.ExternalID, err)
	}

	// Idempotent: ignore callbacks for already-terminal payments
	if payment.IsTerminal() {
		slog.Warn("ignoring callback for terminal payment",
			"payment_id", payment.ID,
			"current_status", payment.Status,
			"callback_status", callback.Status,
		)
		return nil
	}

	callbackJSON, _ := json.Marshal(callback)
	return s.processPaymentOutcome(ctx, payment, callback.IsSuccessful(), callback.FailureReason(), callbackJSON)
}

// HandleMoovCallback processes an incoming Moov Money callback.
func (s *PaymentService) HandleMoovCallback(ctx context.Context, callback moov.CallbackPayload) error {
	slog.Info("received Moov callback",
		"reference", callback.Reference,
		"status", callback.StatusCode,
	)

	payment, err := s.payments.GetByProviderTxID(ctx, callback.Reference)
	if err != nil {
		return fmt.Errorf("payment not found for reference %s: %w", callback.Reference, err)
	}

	if payment.IsTerminal() {
		slog.Warn("ignoring callback for terminal payment",
			"payment_id", payment.ID,
			"current_status", payment.Status,
			"callback_status", callback.StatusCode,
		)
		return nil
	}

	callbackJSON, _ := json.Marshal(callback)
	return s.processPaymentOutcome(ctx, payment, callback.IsSuccessful(), callback.FailureMessage(), callbackJSON)
}

func (s *PaymentService) processPaymentOutcome(ctx context.Context, payment *model.Payment, success bool, failureReason string, rawCallback []byte) error {
	tx, err := s.payments.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Re-fetch with lock to prevent race conditions from duplicate callbacks
	lease, err := s.leases.GetByIDForUpdate(ctx, tx, payment.LeaseID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	payment.CallbackPayload = rawCallback
	payment.UpdatedAt = now

	if success {
		payment.Status = model.PaymentStatusSuccess

		if err := s.payments.Update(ctx, tx, payment); err != nil {
			return err
		}

		// Transition lease: deposit_pending -> deposit_paid
		if lease.Status == model.LeaseStatusDepositPending {
			if err := lease.TransitionTo(model.LeaseStatusDepositPaid); err != nil {
				return err
			}
			if err := s.leases.Update(ctx, tx, lease); err != nil {
				return err
			}
		}

		if err := s.publisher.Publish(ctx, tx, payment.LeaseID, model.EventLeaseDepositPaid, map[string]any{
			"lease_id":   payment.LeaseID,
			"payment_id": payment.ID,
			"amount":     payment.Amount,
			"provider":   payment.Provider,
		}); err != nil {
			return err
		}

		slog.Info("deposit payment successful",
			"payment_id", payment.ID,
			"lease_id", payment.LeaseID,
		)
	} else {
		payment.Status = model.PaymentStatusFailed
		payment.FailureReason = failureReason

		if err := s.payments.Update(ctx, tx, payment); err != nil {
			return err
		}

		// Transition lease back to approved so tenant can retry
		if lease.Status == model.LeaseStatusDepositPending {
			if err := lease.TransitionTo(model.LeaseStatusApproved); err != nil {
				return err
			}
			if err := s.leases.Update(ctx, tx, lease); err != nil {
				return err
			}
		}

		if err := s.publisher.Publish(ctx, tx, payment.LeaseID, model.EventLeaseDepositFailed, map[string]any{
			"lease_id":       payment.LeaseID,
			"payment_id":     payment.ID,
			"failure_reason": failureReason,
			"provider":       payment.Provider,
		}); err != nil {
			return err
		}

		slog.Info("deposit payment failed",
			"payment_id", payment.ID,
			"lease_id", payment.LeaseID,
			"reason", failureReason,
		)
	}

	return tx.Commit(ctx)
}

func (s *PaymentService) GetDepositStatus(ctx context.Context, leaseID string) (*model.Payment, error) {
	return s.payments.GetLatestByLeaseID(ctx, leaseID)
}
