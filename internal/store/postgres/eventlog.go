package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/antiartificial/contextdb/internal/store"
)

// EventLog implements store.EventLog backed by PostgreSQL.
type EventLog struct {
	pool *pgxpool.Pool
}

// NewEventLog returns an EventLog backed by the given connection pool.
func NewEventLog(pool *pgxpool.Pool) *EventLog {
	return &EventLog{pool: pool}
}

func (l *EventLog) Append(ctx context.Context, e store.Event) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.TxTime.IsZero() {
		e.TxTime = time.Now()
	}

	payload := e.Payload
	if payload == nil {
		payload = []byte("{}")
	}
	// validate payload is valid JSON
	if !json.Valid(payload) {
		payload = []byte("{}")
	}

	_, err := l.pool.Exec(ctx, `
		INSERT INTO events (id, namespace, type, payload, tx_time, processed)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, e.ID, e.Namespace, string(e.Type), payload, e.TxTime, e.Processed)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (l *EventLog) Since(ctx context.Context, ns string, after time.Time) ([]store.Event, error) {
	rows, err := l.pool.Query(ctx, `
		SELECT id, namespace, type, payload, tx_time, processed
		FROM events
		WHERE namespace = $1 AND tx_time > $2 AND NOT processed
		ORDER BY tx_time ASC
	`, ns, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []store.Event
	for rows.Next() {
		var e store.Event
		var typ string
		err := rows.Scan(&e.ID, &e.Namespace, &typ, &e.Payload, &e.TxTime, &e.Processed)
		if err != nil {
			return nil, err
		}
		e.Type = store.EventType(typ)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (l *EventLog) MarkProcessed(ctx context.Context, eventID uuid.UUID) error {
	_, err := l.pool.Exec(ctx, "UPDATE events SET processed = TRUE WHERE id = $1", eventID)
	return err
}
