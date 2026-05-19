// Package worker contains background processes.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PaymentExpiry expires pending payments that have not received a callback
// within the configured timeout, and transitions the associated lease back
// to "approved" so the tenant can retry.
type PaymentExpiry struct {
	pool     *pgxpool.Pool
	timeout  time.Duration // How long a payment can stay pending
	interval time.Duration // How often to check
}

func NewPaymentExpiry(pool *pgxpool.Pool, timeout, interval time.Duration) *PaymentExpiry {
	return &PaymentExpiry{
		pool:     pool,
		timeout:  timeout,
		interval: interval,
	}
}

// Run starts the expiry loop. It blocks until the context is cancelled.
func (w *PaymentExpiry) Run(ctx context.Context) {
	slog.Info("payment expiry worker started",
		"timeout", w.timeout,
		"interval", w.interval,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("payment expiry worker stopped")
			return
		case <-ticker.C:
			w.expireStalePayments(ctx)
		}
	}
}

func (w *PaymentExpiry) expireStalePayments(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-w.timeout)

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		slog.Error("expiry worker: begin tx", "error", err)
		return
	}
	defer tx.Rollback(ctx)

	// Find and lock stale pending payments
	rows, err := tx.Query(ctx,
		`SELECT p.id, p.lease_id
		 FROM payments p
		 JOIN leases l ON l.id = p.lease_id
		 WHERE p.status = 'pending'
		   AND p.created_at < $1
		   AND l.status = 'deposit_pending'
		 FOR UPDATE OF p, l SKIP LOCKED`,
		cutoff,
	)
	if err != nil {
		slog.Error("expiry worker: query stale payments", "error", err)
		return
	}
	defer rows.Close()

	type stalePayment struct {
		paymentID string
		leaseID   string
	}
	var stale []stalePayment

	for rows.Next() {
		var sp stalePayment
		if err := rows.Scan(&sp.paymentID, &sp.leaseID); err != nil {
			slog.Error("expiry worker: scan", "error", err)
			return
		}
		stale = append(stale, sp)
	}
	rows.Close()

	if len(stale) == 0 {
		return
	}

	now := time.Now().UTC()
	for _, sp := range stale {
		// Expire the payment
		_, err := tx.Exec(ctx,
			`UPDATE payments SET status = 'expired', failure_reason = 'payment timed out', updated_at = $1 WHERE id = $2`,
			now, sp.paymentID,
		)
		if err != nil {
			slog.Error("expiry worker: expire payment", "error", err, "payment_id", sp.paymentID)
			return
		}

		// Move lease back to approved so tenant can retry
		_, err = tx.Exec(ctx,
			`UPDATE leases SET status = 'approved', updated_at = $1, version = version + 1 WHERE id = $2 AND status = 'deposit_pending'`,
			now, sp.leaseID,
		)
		if err != nil {
			slog.Error("expiry worker: reset lease", "error", err, "lease_id", sp.leaseID)
			return
		}

		slog.Info("expired stale payment",
			"payment_id", sp.paymentID,
			"lease_id", sp.leaseID,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("expiry worker: commit", "error", err)
		return
	}

	slog.Info("expiry worker: processed stale payments", "count", len(stale))
}
