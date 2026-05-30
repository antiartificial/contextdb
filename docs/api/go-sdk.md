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

### ExplainRank

```go
explanation, err := ns.ExplainRank(ctx, client.ExplainRankRequest{
    NodeID:      firstID,
    OtherNodeID: secondID,
    Vector:      queryVector,
})
fmt.Println(explanation.Summary)
for _, factor := range explanation.Factors {
    fmt.Printf("%s: %.3f\n", factor.Factor, factor.Delta)
}
fmt.Printf("supports=%d compound=%.2f\n",
    explanation.Node.Evidence.SupportCount,
    explanation.Node.Evidence.CompoundConfidence,
)
```

`ExplainRank` compares two existing nodes under the namespace scoring model and returns each node's score breakdown, the winner, margin, factor deltas, and support-chain evidence when available.

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

## Feedback

Feedback methods update node versions in place without deleting history:

```go
validated, err := ns.ValidateClaim(ctx, nodeID)
refuted, err := ns.RefuteClaim(ctx, nodeID, "incorrect source")
useful, err := ns.MarkUseful(ctx, nodeID, 5)
stale, err := ns.MarkStale(ctx, nodeID, "superseded")
```

Validation/refutation also update the asserting source when the node has a `source_id`.

Feedback operations append durable audit events. Read them with:

```go
events, err := ns.FeedbackEvents(ctx, time.Now().Add(-24*time.Hour))
for _, event := range events {
    fmt.Printf("%s %s %s\n", event.TxTime, event.Action, event.NodeID)
}
```

Source trust timelines are derived from feedback events that update source credibility:

```go
points, err := ns.SourceTrustTimeline(ctx, "docs-crawler", time.Time{})
for _, point := range points {
    fmt.Printf("%s %.2f\n", point.Action, point.SourceCredibility)
}
```

Review queues derive operator tasks from feedback, low-confidence claims, and contradictions:

```go
items, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{
    After:                     time.Now().Add(-24 * time.Hour),
    LowConfidenceThreshold:    0.35,
    SourceTrustDropThreshold:  0.2,
    SourceRefutationThreshold: 2,
    Types:                     []string{"source_trust_anomaly"},
    SourceID:                  "docs-crawler",
    Status:                    "open",
    Limit:                     20,
})
for _, item := range items {
    fmt.Printf("%s %.2f %s\n", item.Type, item.Priority, item.Suggested)
}
```

Source trust anomaly tasks are emitted as `ReviewItem{Type: "source_trust_anomaly"}` when configured source credibility thresholds are crossed. Review queue filters can narrow by item type, source, workflow status, and owner; items with no recorded decision match `Status: "open"`.

Review decisions persist workflow state without making queue generation stateful:

```go
decision, err := ns.RecordReviewDecision(ctx, client.ReviewDecisionRequest{
    ReviewID: "low_confidence:550e8400-e29b-41d4-a716-446655440000",
    Status:   "assigned",
    Owner:    "alice",
    Decision: "needs_evidence",
    Note:     "check source logs",
})
decisions, err := ns.ReviewDecisions(ctx, time.Now().Add(-24*time.Hour))
```

Supported statuses are `open`, `assigned`, `resolved`, and `snoozed`. The derived queue overlays the latest decision for each task; resolved tasks are hidden, and snoozed tasks are hidden until `RecheckAt`.

## Narrative And Gaps

```go
report, err := ns.Explain(ctx, nodeID)
gaps, err := ns.KnowledgeGaps(ctx, client.GapRequest{
    TopK:       20,
    MinGapSize: 0.5,
    MaxGaps:    10,
})
plan, err := ns.AcquisitionPlan(ctx, client.AcquisitionPlanRequest{
    Budget: 5,
    MaxGaps: 3,
})
```

`Explain` returns a structured narrative report with evidence, contradictions, provenance, and confidence explanation. `KnowledgeGaps` returns sparse semantic regions that suggest where the namespace needs more information. `AcquisitionPlan` turns gaps and weak claims into prioritized research, crawl, verification, or refresh tasks.

Sets labels on a source. Use "moderator"/"admin" for full trust, "troll"/"flagged" for floor.

## Export / Import

The client provides namespace export and import via NDJSON:

```go
var buf bytes.Buffer
err := db.ExportSnapshot(ctx, "my-app", &buf)

// Export a filtered subgraph from seed nodes.
err = db.ExportSnapshotFromSeeds(ctx, "my-app", seedIDs, 3, &buf)

// Validate without writing, then import into another namespace.
dryRun, err := db.ValidateSnapshotReport(ctx, "restore-preview", bytes.NewReader(buf.Bytes()))
report, err := db.ImportSnapshotReport(ctx, "restore-preview", bytes.NewReader(buf.Bytes()))
fmt.Printf("nodes=%d vectors=%d overrides=%d\n", report.Nodes, report.Vectors, dryRun.NamespaceOverrides)
```

The NDJSON format contains one record per line:

```json
{"type":"node","data":{...}}
{"type":"edge","data":{...}}
{"type":"source","data":{...}}
```
