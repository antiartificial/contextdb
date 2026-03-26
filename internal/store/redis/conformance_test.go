//go:build integration
// +build integration

package redis

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/store"
)

func TestRedisEventLog(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	el, err := NewEventLog(redisAddr(), "test:events:")
	is.NoErr(err)
	defer el.Close()

	eventID := uuid.New()
	ns := "test-" + uuid.NewString()[:8]

	// Append
	err = el.Append(ctx, store.Event{
		ID:        eventID,
		Namespace: ns,
		Type:      store.EventNodeUpsert,
		Payload:   []byte(`{"id":"test"}`),
		TxTime:    time.Now(),
	})
	is.NoErr(err)

	// Since
	events, err := el.Since(ctx, ns, time.Now().Add(-1*time.Minute))
	is.NoErr(err)
	is.True(len(events) >= 1)

	// MarkProcessed
	err = el.MarkProcessed(ctx, eventID)
	is.NoErr(err)
}
