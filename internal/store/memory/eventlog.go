package memory

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/store"
)

// EventLog is an append-only in-memory event log.
type EventLog struct {
	mu     sync.RWMutex
	events []store.Event
}

func NewEventLog() *EventLog {
	return &EventLog{}
}

func (l *EventLog) Append(_ context.Context, e store.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.TxTime.IsZero() {
		e.TxTime = time.Now()
	}
	l.events = append(l.events, e)
	return nil
}

func (l *EventLog) Since(_ context.Context, ns string, after time.Time) ([]store.Event, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var out []store.Event
	for _, e := range l.events {
		if e.Namespace == ns && e.TxTime.After(after) && !e.Processed {
			out = append(out, e)
		}
	}
	return out, nil
}

func (l *EventLog) SinceAll(_ context.Context, ns string, after time.Time) ([]store.Event, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var out []store.Event
	for _, e := range l.events {
		if e.Namespace == ns && e.TxTime.After(after) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (l *EventLog) MarkProcessed(_ context.Context, eventID uuid.UUID) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i, e := range l.events {
		if e.ID == eventID {
			l.events[i].Processed = true
			return nil
		}
	}
	return nil
}
