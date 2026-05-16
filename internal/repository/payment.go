package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
)

type PaymentRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentRepository(pool *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{pool: pool}
}

func (r *PaymentRepository) Create(ctx context.Context, tx pgx.Tx, p *model.Payment) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO payments (id, lease_id, tenant_id, amount, provider, provider_tx_id, phone_number, status, created_at, updated_at, version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		p.ID, p.LeaseID, p.TenantID, p.Amount, p.Provider, p.ProviderTxID,
		p.PhoneNumber, p.Status, p.CreatedAt, p.UpdatedAt, p.Version,
	)
	return err
}

func (r *PaymentRepository) GetByID(ctx context.Context, id string) (*model.Payment, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, lease_id, tenant_id, amount, provider, provider_tx_id, phone_number,
		        status, failure_reason, callback_payload, created_at, updated_at, version
		 FROM payments WHERE id = $1`, id)
	return scanPayment(row)
}

func (r *PaymentRepository) GetByProviderTxID(ctx context.Context, providerTxID string) (*model.Payment, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, lease_id, tenant_id, amount, provider, provider_tx_id, phone_number,
		        status, failure_reason, callback_payload, created_at, updated_at, version
		 FROM payments WHERE provider_tx_id = $1`, providerTxID)
	return scanPayment(row)
}

func (r *PaymentRepository) GetPendingByLeaseID(ctx context.Context, leaseID string) (*model.Payment, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, lease_id, tenant_id, amount, provider, provider_tx_id, phone_number,
		        status, failure_reason, callback_payload, created_at, updated_at, version
		 FROM payments WHERE lease_id = $1 AND status = 'pending'
		 ORDER BY created_at DESC LIMIT 1`, leaseID)
	return scanPayment(row)
}

func (r *PaymentRepository) GetLatestByLeaseID(ctx context.Context, leaseID string) (*model.Payment, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, lease_id, tenant_id, amount, provider, provider_tx_id, phone_number,
		        status, failure_reason, callback_payload, created_at, updated_at, version
		 FROM payments WHERE lease_id = $1
		 ORDER BY created_at DESC LIMIT 1`, leaseID)
	return scanPayment(row)
}

func (r *PaymentRepository) Update(ctx context.Context, tx pgx.Tx, p *model.Payment) error {
	result, err := tx.Exec(ctx,
		`UPDATE payments
		 SET status = $1, failure_reason = $2, callback_payload = $3, updated_at = $4, version = version + 1
		 WHERE id = $5 AND version = $6`,
		p.Status, p.FailureReason, p.CallbackPayload, p.UpdatedAt, p.ID, p.Version,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("optimistic lock conflict: payment was modified concurrently")
	}
	p.Version++
	return nil
}

func (r *PaymentRepository) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

func scanPayment(row pgx.Row) (*model.Payment, error) {
	var p model.Payment
	err := row.Scan(
		&p.ID, &p.LeaseID, &p.TenantID, &p.Amount, &p.Provider, &p.ProviderTxID,
		&p.PhoneNumber, &p.Status, &p.FailureReason, &p.CallbackPayload,
		&p.CreatedAt, &p.UpdatedAt, &p.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrPaymentNotFound
		}
		return nil, err
	}
	return &p, nil
}
