package badger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/store"
)

const prefixEvent = "ev/"

// EventLog implements store.EventLog backed by BadgerDB.
type EventLog struct {
	db *badgerdb.DB
}

// NewEventLog returns an EventLog backed by the given BadgerDB instance.
func NewEventLog(db *badgerdb.DB) *EventLog {
	return &EventLog{db: db}
}

func eventKey(ns string, txTime time.Time, id uuid.UUID) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s", prefixEvent, ns, txTime.UnixNano(), id))
}

func eventNSPrefix(ns string) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixEvent, ns))
}

func (l *EventLog) Append(_ context.Context, e store.Event) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.TxTime.IsZero() {
		e.TxTime = time.Now()
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	return l.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(eventKey(e.Namespace, e.TxTime, e.ID), data)
	})
}

func (l *EventLog) Since(_ context.Context, ns string, after time.Time) ([]store.Event, error) {
	var events []store.Event
	prefix := eventNSPrefix(ns)
	seekKey := []byte(fmt.Sprintf("%s%s/%020d/", prefixEvent, ns, after.UnixNano()))

	err := l.db.View(func(txn *badgerdb.Txn) error {
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(seekKey); it.ValidForPrefix(prefix); it.Next() {
			var e store.Event
			if err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &e)
			}); err != nil {
				continue
			}
			if e.TxTime.After(after) && !e.Processed {
				events = append(events, e)
			}
		}
		return nil
	})
	return events, err
}

func (l *EventLog) MarkProcessed(_ context.Context, eventID uuid.UUID) error {
	return l.db.Update(func(txn *badgerdb.Txn) error {
		// scan all events to find by ID
		prefix := []byte(prefixEvent)
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var e store.Event
			if err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &e)
			}); err != nil {
				continue
			}
			if e.ID == eventID {
				e.Processed = true
				data, err := json.Marshal(e)
				if err != nil {
					return err
				}
				return txn.Set(it.Item().KeyCopy(nil), data)
			}
		}
		return nil
	})
}
