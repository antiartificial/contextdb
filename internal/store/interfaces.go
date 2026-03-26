package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
)

// GraphStore manages nodes and edges with full bi-temporal semantics.
// All writes are non-destructive — invalidation sets a timestamp rather
// than removing data so temporal queries remain correct.
type GraphStore interface {
	// UpsertNode writes a node, creating or incrementing its version.
	UpsertNode(ctx context.Context, n core.Node) error

	// GetNode returns the current (highest-version) node by ID.
	GetNode(ctx context.Context, ns string, id uuid.UUID) (*core.Node, error)

	// AsOf returns the node as it existed at the given valid-time anchor.
	// Returns nil, nil if no version was valid at that time.
	AsOf(ctx context.Context, ns string, id uuid.UUID, t time.Time) (*core.Node, error)

	// History returns all versions of a node, oldest first.
	History(ctx context.Context, ns string, id uuid.UUID) ([]core.Node, error)

	// UpsertEdge writes an edge.
	UpsertEdge(ctx context.Context, e core.Edge) error

	// InvalidateEdge sets InvalidatedAt on an edge (non-destructive delete).
	InvalidateEdge(ctx context.Context, ns string, id uuid.UUID, at time.Time) error

	// GetEdges returns all currently active edges originating from nodeID.
	GetEdges(ctx context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error)

	// GetEdgesTo returns all currently active edges pointing at nodeID.
	GetEdgesTo(ctx context.Context, ns string, nodeID uuid.UUID) ([]core.Edge, error)

	// EdgesFrom returns all currently active edges originating from nodeID.
	// edgeTypes filters by type; nil = all types.
	EdgesFrom(ctx context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error)

	// EdgesTo returns all currently active edges pointing at nodeID.
	EdgesTo(ctx context.Context, ns string, nodeID uuid.UUID, edgeTypes []string) ([]core.Edge, error)

	// Walk traverses the graph from seed nodes according to the query params.
	Walk(ctx context.Context, q WalkQuery) ([]core.Node, error)

	// UpsertSource writes a source record.
	UpsertSource(ctx context.Context, s core.Source) error

	// GetSource returns a source by external ID.
	GetSourceByExternalID(ctx context.Context, ns, externalID string) (*core.Source, error)

	// UpdateCredibility applies a delta to a source's credibility score,
	// clamped to [0, 1].
	UpdateCredibility(ctx context.Context, ns string, id uuid.UUID, delta float64) error
}

// WalkQuery parameterises a graph traversal.
type WalkQuery struct {
	Namespace string
	SeedIDs   []uuid.UUID
	EdgeTypes []string // nil = all types
	MaxDepth  int
	Strategy  TraversalStrategy
	AsOf      time.Time // zero = now
	MinWeight float64   // prune edges below this weight; 0 = no pruning
}

// TraversalStrategy selects the graph walk algorithm.
type TraversalStrategy string

const (
	StrategyBFS         TraversalStrategy = "bfs"
	StrategyWaterCircle TraversalStrategy = "water_circle"
	StrategyBeam        TraversalStrategy = "beam"
)

// VectorIndex manages embedding vectors and ANN search.
type VectorIndex interface {
	// Index stores or replaces a vector entry.
	Index(ctx context.Context, entry core.VectorEntry) error

	// Delete removes a vector entry by ID.
	Delete(ctx context.Context, ns string, id uuid.UUID) error

	// Search returns the top-K most similar entries to the query vector.
	// filter is an optional label/property predicate (nil = no filter).
	Search(ctx context.Context, q VectorQuery) ([]core.ScoredNode, error)
}

// VectorQuery parameterises an ANN search.
type VectorQuery struct {
	Namespace string
	Vector    []float32
	TopK      int
	Labels    []string  // if non-empty, only return nodes carrying all labels
	AsOf      time.Time // temporal anchor; zero = now
}

// KVStore is the hot cache for active namespace context.
type KVStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttlSeconds int) error
	Delete(ctx context.Context, key string) error
}

// EventLog is an append-only write-ahead log used by compaction workers.
type EventLog interface {
	Append(ctx context.Context, event Event) error
	Since(ctx context.Context, ns string, after time.Time) ([]Event, error)
	MarkProcessed(ctx context.Context, eventID uuid.UUID) error
}

// EventType enumerates the kinds of writes recorded in the event log.
type EventType string

const (
	EventNodeUpsert     EventType = "node_upsert"
	EventEdgeUpsert     EventType = "edge_upsert"
	EventEdgeInvalidate EventType = "edge_invalidate"
	EventSourceUpdate   EventType = "source_update"
)

// Event is a single append-only log record.
type Event struct {
	ID        uuid.UUID
	Namespace string
	Type      EventType
	Payload   []byte // JSON-encoded core type
	TxTime    time.Time
	Processed bool
}
