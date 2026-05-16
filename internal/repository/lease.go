package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
)

type LeaseRepository struct {
	pool *pgxpool.Pool
}

func NewLeaseRepository(pool *pgxpool.Pool) *LeaseRepository {
	return &LeaseRepository{pool: pool}
}

func (r *LeaseRepository) GetByID(ctx context.Context, id string) (*model.Lease, error) {
	return r.getByID(ctx, r.pool, id)
}

func (r *LeaseRepository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*model.Lease, error) {
	return r.getByIDTx(ctx, tx, id)
}

func (r *LeaseRepository) getByID(ctx context.Context, q querier, id string) (*model.Lease, error) {
	row := q.QueryRow(ctx,
		`SELECT id, tenant_id, property_id, rent_amount, deposit_amount, status,
		        signed_at, activated_at, created_at, updated_at, version
		 FROM leases WHERE id = $1`, id)
	return scanLease(row)
}

func (r *LeaseRepository) getByIDTx(ctx context.Context, tx pgx.Tx, id string) (*model.Lease, error) {
	row := tx.QueryRow(ctx,
		`SELECT id, tenant_id, property_id, rent_amount, deposit_amount, status,
		        signed_at, activated_at, created_at, updated_at, version
		 FROM leases WHERE id = $1 FOR UPDATE`, id)
	return scanLease(row)
}

func (r *LeaseRepository) Update(ctx context.Context, tx pgx.Tx, lease *model.Lease) error {
	result, err := tx.Exec(ctx,
		`UPDATE leases
		 SET status = $1, signed_at = $2, activated_at = $3, updated_at = $4, version = version + 1
		 WHERE id = $5 AND version = $6`,
		lease.Status, lease.SignedAt, lease.ActivatedAt, lease.UpdatedAt, lease.ID, lease.Version,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("optimistic lock conflict: lease was modified concurrently")
	}
	lease.Version++
	return nil
}

func (r *LeaseRepository) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

func scanLease(row pgx.Row) (*model.Lease, error) {
	var l model.Lease
	err := row.Scan(
		&l.ID, &l.TenantID, &l.PropertyID, &l.RentAmount, &l.DepositAmount,
		&l.Status, &l.SignedAt, &l.ActivatedAt, &l.CreatedAt, &l.UpdatedAt, &l.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrLeaseNotFound
		}
		return nil, err
	}
	return &l, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
