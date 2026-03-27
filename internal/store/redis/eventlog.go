//go:build integration
// +build integration

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/antiartificial/contextdb/internal/store"
)

// EventLog implements store.EventLog using Redis Streams.
// Uses XADD/XREAD/XACK for append-only event log semantics.
type EventLog struct {
	client *redis.Client
	prefix string
}

// NewEventLog creates an EventLog backed by Redis Streams.
func NewEventLog(addr, prefix string) (*EventLog, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	if prefix == "" {
		prefix = "contextdb:events:"
	}
	return &EventLog{client: client, prefix: prefix}, nil
}

// Close closes the Redis connection.
func (e *EventLog) Close() error {
	return e.client.Close()
}

func (e *EventLog) streamKey(ns string) string {
	return e.prefix + ns
}

func (e *EventLog) Append(ctx context.Context, event store.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	return e.client.XAdd(ctx, &redis.XAddArgs{
		Stream: e.streamKey(event.Namespace),
		Values: map[string]interface{}{
			"id":   event.ID.String(),
			"data": string(data),
		},
	}).Err()
}

func (e *EventLog) Since(ctx context.Context, ns string, after time.Time) ([]store.Event, error) {
	// Use millisecond timestamp as Redis stream ID
	startID := fmt.Sprintf("%d-0", after.UnixMilli())

	msgs, err := e.client.XRange(ctx, e.streamKey(ns), startID, "+").Result()
	if err != nil {
		return nil, fmt.Errorf("xrange: %w", err)
	}

	events := make([]store.Event, 0, len(msgs))
	for _, msg := range msgs {
		data, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}
		var event store.Event
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

func (e *EventLog) SinceAll(ctx context.Context, ns string, after time.Time) ([]store.Event, error) {
	// Same as Since but returns all events regardless of processed state.
	startID := fmt.Sprintf("%d-0", after.UnixMilli())

	msgs, err := e.client.XRange(ctx, e.streamKey(ns), startID, "+").Result()
	if err != nil {
		return nil, fmt.Errorf("xrange: %w", err)
	}

	events := make([]store.Event, 0, len(msgs))
	for _, msg := range msgs {
		data, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}
		var event store.Event
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

func (e *EventLog) MarkProcessed(ctx context.Context, eventID uuid.UUID) error {
	// In a full implementation, we'd use consumer groups and XACK.
	// For now, we store processed status in a separate set.
	return e.client.SAdd(ctx, e.prefix+"processed", eventID.String()).Err()
}
