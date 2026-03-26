---
title: Go SDK
parent: API Reference
nav_order: 1
---

# Go SDK

The Go SDK in `pkg/client` is the primary interface to contextdb. It follows `database/sql` conventions -- open one `DB`, share it across your application.

## Installation

```bash
go get github.com/antiartificial/contextdb@latest
```

## Opening a connection

```go
import "github.com/antiartificial/contextdb/pkg/client"

// In-memory (no persistence)
db, err := client.Open(client.Options{})

// Persistent embedded (BadgerDB)
db, err := client.Open(client.Options{
    DataDir: "/var/lib/contextdb",
})

// Postgres
db, err := client.Open(client.Options{
    Mode: client.ModeStandard,
    DSN:  "postgres://user:pass@localhost:5432/contextdb?sslmode=disable",
})

// MustOpen panics on error (useful in main/tests)
db := client.MustOpen(client.Options{})
```

## Options

| Field | Type | Default | Description |
|:------|:-----|:--------|:------------|
| `Mode` | `Mode` | `"embedded"` | Storage backend: `embedded`, `standard`, `remote` |
| `DataDir` | `string` | `""` | BadgerDB directory (empty = in-memory) |
| `DSN` | `string` | `""` | Postgres connection string |
| `Addr` | `string` | `""` | Remote server address |
| `Logger` | `*slog.Logger` | `slog.Default()` | Structured logger |
| `MaxOpenConns` | `int` | `10` | Postgres connection pool size |
| `ConnectTimeout` | `time.Duration` | `5s` | Backend connection timeout |
| `Extractor` | `extract.Extractor` | `nil` | LLM entity extractor for IngestText |
| `LLMProvider` | `extract.Provider` | `nil` | LLM provider for extraction/compaction |

## DB methods

### `Namespace(name string, mode namespace.Mode) *NamespaceHandle`

Returns a handle scoped to the named namespace. Creates the namespace if it doesn't exist. Subsequent calls with the same name return the same handle.

```go
ns := db.Namespace("channel:general", namespace.ModeBeliefSystem)
```

### `Ping(ctx context.Context) error`

Checks the connection is alive.

### `Stats() DBStats`

Returns runtime statistics: retrieval/ingest counters, latency percentiles.

### `Close() error`

Releases all resources. The DB is unusable after Close.

## NamespaceHandle methods

### Write

```go
func (h *NamespaceHandle) Write(ctx context.Context, req WriteRequest) (WriteResult, error)
```

Ingests a single item. Runs through the admission gate before persisting.

**WriteRequest fields:**

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `Content` | `string` | Yes | The text content |
| `SourceID` | `string` | Yes | External source identifier |
| `Labels` | `[]string` | No | Node labels (e.g. "Claim", "Skill") |
| `Properties` | `map[string]any` | No | Arbitrary metadata |
| `Vector` | `[]float32` | No | Pre-computed embedding |
| `ModelID` | `string` | No | Embedding model identifier |
| `Confidence` | `float64` | No | Initial confidence [0,1] |
| `ValidFrom` | `time.Time` | No | When the fact became true (default: now) |
| `MemType` | `core.MemoryType` | No | Memory type for decay |

**WriteResult fields:**

| Field | Type | Description |
|:------|:-----|:------------|
| `NodeID` | `uuid.UUID` | The written (or existing duplicate) node ID |
| `Admitted` | `bool` | Whether the write was accepted |
| `Reason` | `string` | Rejection reason (if not admitted) |
| `ConflictIDs` | `[]uuid.UUID` | IDs of contradicting nodes |

### WriteBatch

```go
func (h *NamespaceHandle) WriteBatch(ctx context.Context, reqs []WriteRequest) ([]WriteResult, error)
```

Writes multiple items. Partial failures are possible -- results are returned in request order. The first error is returned alongside partial results.

### Retrieve

```go
func (h *NamespaceHandle) Retrieve(ctx context.Context, req RetrieveRequest) ([]Result, error)
```

Runs hybrid retrieval (vector + graph + session) and returns scored results.

**RetrieveRequest fields:**

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `Vector` | `[]float32` | For vector search | Query embedding |
| `SeedIDs` | `[]uuid.UUID` | For graph walk | Known relevant node IDs |
| `TopK` | `int` | No | Max results (default: 10) |
| `ScoreParams` | `core.ScoreParams` | No | Override scoring weights |
| `AsOf` | `time.Time` | No | Point-in-time query (default: now) |

**Result fields:**

| Field | Type | Description |
|:------|:-----|:------------|
| `Node` | `core.Node` | The full node |
| `Score` | `float64` | Composite score [0, 1] |
| `SimilarityScore` | `float64` | Vector similarity component |
| `ConfidenceScore` | `float64` | Confidence component |
| `RecencyScore` | `float64` | Recency component |
| `UtilityScore` | `float64` | Utility component |
| `RetrievalSource` | `string` | "vector", "graph", or "fused" |

### GetNode

```go
func (h *NamespaceHandle) GetNode(ctx context.Context, id uuid.UUID) (*core.Node, error)
```

Retrieves a single node by ID.

### Walk

```go
func (h *NamespaceHandle) Walk(ctx context.Context, seedIDs []uuid.UUID, maxDepth int) ([]WalkResult, error)
```

Breadth-first graph traversal from seed nodes. Returns nodes with depth and path information.

**WalkResult fields:**

| Field | Type | Description |
|:------|:-----|:------------|
| `Node` | `core.Node` | The discovered node |
| `Depth` | `int` | Hops from nearest seed |
| `Path` | `[]uuid.UUID` | Node IDs from seed to this node |

### AddEdge

```go
func (h *NamespaceHandle) AddEdge(ctx context.Context, edge core.Edge) error
```

Creates a directed edge between two nodes. Auto-fills namespace, ID, and timestamps if empty.

### History

```go
func (h *NamespaceHandle) History(ctx context.Context, nodeID uuid.UUID) ([]core.Node, error)
```

Returns all versions of a node, ordered oldest-first by transaction time.

### IngestText

```go
func (h *NamespaceHandle) IngestText(ctx context.Context, text, sourceID string) (*ingest.IngestResult, error)
```

Runs text through the LLM extraction pipeline to automatically produce nodes and edges. Requires `Options.Extractor` to be configured.

### LabelSource

```go
func (h *NamespaceHandle) LabelSource(ctx context.Context, externalID string, labels []string) error
```

Sets labels on a source. Use "moderator"/"admin" for full trust, "troll"/"flagged" for floor.
