---
title: Go SDK
---

# Go SDK

The Go SDK in `pkg/client` is the primary interface to contextdb. It follows `database/sql` conventions: open one `DB`, share it across your application.

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

// Remote (gRPC to contextdb server)
db, err := client.Open(client.Options{
    Mode: client.ModeRemote,
    Addr: "localhost:7700",
})

// Scaled (Qdrant + Redis + Postgres)
db, err := client.Open(client.Options{
    Mode:       client.ModeScaled,
    DSN:        "postgres://user:pass@localhost:5432/contextdb?sslmode=disable",
    QdrantAddr: "localhost:6334",
    RedisAddr:  "localhost:6379",
})

// With auto-embedding
db, err := client.Open(client.Options{
    Embedder:   embedding.NewOpenAI("https://api.openai.com/v1", apiKey, "text-embedding-3-small", 1536),
    EmbedModel: "text-embedding-3-small",
})

// MustOpen panics on error (useful in main/tests)
db := client.MustOpen(client.Options{})
```

## Options

| Field | Type | Default | Description |
|:------|:-----|:--------|:------------|
| `Mode` | `Mode` | `"embedded"` | Storage backend: `embedded`, `standard`, `remote`, `scaled` |
| `DataDir` | `string` | `""` | BadgerDB directory (empty = in-memory) |
| `DSN` | `string` | `""` | Postgres connection string |
| `Addr` | `string` | `""` | Remote server address (for `ModeRemote`) |
| `QdrantAddr` | `string` | `""` | Qdrant gRPC address (for `ModeScaled`) |
| `RedisAddr` | `string` | `""` | Redis address (for `ModeScaled`) |
| `ObserveAddr` | `string` | `":7702"` | Metrics/pprof server bind address |
| `Logger` | `*slog.Logger` | `slog.Default()` | Structured logger |
| `MaxOpenConns` | `int` | `10` | Postgres connection pool size |
| `ConnectTimeout` | `time.Duration` | `5s` | Backend connection timeout |
| `Extractor` | `extract.Extractor` | `nil` | LLM entity extractor for IngestText |
| `LLMProvider` | `extract.Provider` | `nil` | LLM provider for extraction/compaction |
| `Embedder` | `embedding.Embedder` | `nil` | Auto-embedding provider |
| `EmbedModel` | `string` | `""` | Embedding model identifier (provenance) |
| `VectorDimensions` | `int` | `1536` | Embedding dimensionality |

## Mode constants

```go
const (
    ModeEmbedded Mode = "embedded"  // In-process, zero external deps
    ModeStandard Mode = "standard"  // Postgres + pgvector
    ModeRemote   Mode = "remote"    // gRPC to contextdb server
    ModeScaled   Mode = "scaled"    // Qdrant + Redis + Postgres
)
```

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

### `Stores() (GraphStore, VectorIndex, KVStore, EventLog)`

Returns direct access to the underlying store implementations. Useful for advanced operations and testing.

### `Registry() *observe.Registry`

Returns the observability registry for custom metric registration.

### `Close() error`

Releases all resources. The DB is unusable after Close.

## NamespaceHandle methods

### Write

```go
func (h *NamespaceHandle) Write(ctx context.Context, req WriteRequest) (WriteResult, error)
```

Ingests a single item. Runs through auto-embedding, the admission gate, and conflict detection before persisting.

**WriteRequest fields:**

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `Content` | `string` | Yes | The text content |
| `SourceID` | `string` | Yes | External source identifier |
| `Labels` | `[]string` | No | Node labels (e.g. "Claim", "Skill") |
| `Properties` | `map[string]any` | No | Arbitrary metadata |
| `Vector` | `[]float32` | No | Pre-computed embedding (auto-embedded from Content if omitted and Embedder configured) |
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

Writes multiple items. Partial failures are possible. Results are returned in request order. The first error is returned alongside partial results.

### Retrieve

```go
func (h *NamespaceHandle) Retrieve(ctx context.Context, req RetrieveRequest) ([]Result, error)
```

Runs hybrid retrieval (vector + graph + session) with optional reranking and returns scored results.

**RetrieveRequest fields:**

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `Vector` | `[]float32` | For vector search | Query embedding |
| `Text` | `string` | For text search | Auto-embedded query (requires Embedder) |
| `Vectors` | `[][]float32` | For multi-vector | Multiple query embeddings fused |
| `SeedIDs` | `[]uuid.UUID` | For graph walk | Known relevant node IDs |
| `TopK` | `int` | No | Max results (default: 10) |
| `Labels` | `[]string` | No | Filter to nodes with all specified labels |
| `ScoreParams` | `core.ScoreParams` | No | Override scoring weights |
| `Strategy` | `retrieval.HybridStrategy` | No | Override retrieval strategy |
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
| `RetrievalSource` | `string` | "vector", "graph", "session", or "fused" |

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

## Export / Import

The `snapshot` package provides namespace export and import via NDJSON:

```go
import "github.com/antiartificial/contextdb/internal/snapshot"

// Export a namespace
graph, vecs, _, _ := db.Stores()
exporter := snapshot.NewExporter(graph)

var buf bytes.Buffer
err := exporter.Export(ctx, "my-app", &buf)

// Export subgraph from seeds
err = exporter.ExportFromSeeds(ctx, "my-app", seedIDs, 3, &buf)

// Import into another DB
importer := snapshot.NewImporter(graph, vecs)
err = importer.Import(ctx, "my-app", &buf)
```

The NDJSON format contains one record per line:

```json
{"type":"node","data":{...}}
{"type":"edge","data":{...}}
{"type":"source","data":{...}}
```
