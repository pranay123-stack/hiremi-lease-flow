package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pranay123-stack/hiremi-lease-flow/internal/model"
)

// Publisher defines the interface for publishing domain events.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, aggregateID, eventType string, payload any) error
}

// PostgresOutbox implements the transactional outbox pattern.
// Events are written to an outbox table within the same transaction as
// the domain state change. A separate process (or polling) delivers them.
// This guarantees events are never lost even if the app crashes after commit.
type PostgresOutbox struct {
	pool *pgxpool.Pool
}

func NewPostgresOutbox(pool *pgxpool.Pool) *PostgresOutbox {
	return &PostgresOutbox{pool: pool}
}

func (p *PostgresOutbox) Publish(ctx context.Context, tx pgx.Tx, aggregateID, eventType string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	event := model.OutboxEvent{
		ID:          uuid.New().String(),
		AggregateID: aggregateID,
		EventType:   eventType,
		Payload:     data,
		CreatedAt:   time.Now().UTC(),
		Published:   false,
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO outbox_events (id, aggregate_id, event_type, payload, created_at, published)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ID, event.AggregateID, event.EventType, event.Payload, event.CreatedAt, event.Published,
	)
	if err != nil {
		return err
	}

	slog.Info("event published to outbox",
		"event_id", event.ID,
		"event_type", eventType,
		"aggregate_id", aggregateID,
	)
	return nil
}
