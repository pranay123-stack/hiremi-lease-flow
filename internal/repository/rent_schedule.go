package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
)

type RentScheduleRepository struct{}

func NewRentScheduleRepository() *RentScheduleRepository {
	return &RentScheduleRepository{}
}

func (r *RentScheduleRepository) Create(ctx context.Context, tx pgx.Tx, schedule *model.RentSchedule) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO rent_schedules (id, lease_id, tenant_id, amount, due_date, period, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		schedule.ID, schedule.LeaseID, schedule.TenantID, schedule.Amount,
		schedule.DueDate, schedule.Period, schedule.CreatedAt,
	)
	return err
}
