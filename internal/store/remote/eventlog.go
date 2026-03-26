package remote

import (
	"context"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/antiartificial/contextdb/internal/store"
)

// EventLogStore implements store.EventLog via gRPC.
type EventLogStore struct {
	conn *grpc.ClientConn
}

func (e *EventLogStore) Append(ctx context.Context, event store.Event) error {
	var resp struct{}
	return invoke(ctx, e.conn, "EventAppend", &struct {
		Event store.Event `json:"event"`
	}{Event: event}, &resp)
}

func (e *EventLogStore) Since(ctx context.Context, ns string, after time.Time) ([]store.Event, error) {
	var resp struct {
		Events []store.Event `json:"events"`
	}
	err := invoke(ctx, e.conn, "EventSince", &struct {
		Namespace string    `json:"namespace"`
		After     time.Time `json:"after"`
	}{Namespace: ns, After: after}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Events, nil
}

func (e *EventLogStore) MarkProcessed(ctx context.Context, eventID uuid.UUID) error {
	var resp struct{}
	return invoke(ctx, e.conn, "EventMarkProcessed", &struct {
		EventID string `json:"event_id"`
	}{EventID: eventID.String()}, &resp)
}
