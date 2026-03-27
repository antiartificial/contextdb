package federation

import "sync/atomic"

// Metrics tracks federation activity counters.
// All fields are safe for concurrent use without additional locking.
type Metrics struct {
	// EventsReplicated counts events successfully pulled from remote peers.
	EventsReplicated atomic.Int64

	// EventsSkipped counts events skipped because they were already present
	// (watermark already past the event's TxTime).
	EventsSkipped atomic.Int64

	// PeersActive is the current number of alive peers in the registry.
	PeersActive atomic.Int64

	// SyncErrors counts replication or anti-entropy errors.
	SyncErrors atomic.Int64
}
