// Package client provides the public API for contextdb.
//
// The entry point is [Open], which returns a [DB] — modelled deliberately
// after database/sql so the usage pattern is familiar to any Go developer:
//
//	db, err := client.Open(client.Options{Mode: client.ModeEmbedded})
//	if err != nil { ... }
//	defer db.Close()
//
//	ns := db.Namespace("channel:general", namespace.ModeBeliefSystem)
//
//	// Write
//	id, err := ns.Write(ctx, client.WriteRequest{
//	    Content:    "Go uses a concurrent mark-and-sweep GC",
//	    SourceID:   "moderator:alice",
//	    Labels:     []string{"Claim"},
//	    Confidence: 0.95,
//	})
//
//	// Read
//	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
//	    Vector: embedding,
//	    TopK:   10,
//	})
//
// Deployment modes:
//
//   - [ModeEmbedded]  — in-process, zero external dependencies (default)
//   - [ModeStandard]  — Postgres + pgvector (connect via DSN)
//   - [ModeRemote]    — connect to a running contextdb server over HTTP
package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/embedding"
	"github.com/antiartificial/contextdb/internal/extract"
	"github.com/antiartificial/contextdb/internal/ingest"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/snapshot"
	"github.com/antiartificial/contextdb/internal/store"
	badgerstore "github.com/antiartificial/contextdb/internal/store/badger"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
	pgstore "github.com/antiartificial/contextdb/internal/store/postgres"
	remotestore "github.com/antiartificial/contextdb/internal/store/remote"
)

// Mode selects the storage backend.
type Mode string

const (
	// ModeEmbedded runs entirely in-process. No external dependencies.
	// Suitable for development, testing, and sidecar deployments.
	ModeEmbedded Mode = "embedded"

	// ModeStandard connects to Postgres with the pgvector extension.
	// Set Options.DSN to the connection string.
	// Phase 2 — not yet implemented; falls back to embedded.
	ModeStandard Mode = "standard"

	// ModeRemote connects to a running contextdb server over HTTP.
	// Set Options.Addr to the server address.
	ModeRemote Mode = "remote"

	// ModeScaled uses Qdrant for vectors, Redis for KV/sessions,
	// and Postgres for the graph store. Set QdrantAddr, RedisAddr,
	// and DSN in Options.
	ModeScaled Mode = "scaled"
)

// Options configures the DB connection.
type Options struct {
	// Mode selects the storage backend. Defaults to ModeEmbedded.
	Mode Mode

	// DSN is the Postgres connection string for ModeStandard.
	// Example: "postgres://user:pass@localhost:5432/contextdb?sslmode=disable"
	DSN string

	// Addr is the contextdb server address for ModeRemote.
	// Example: "http://localhost:7700"
	Addr string

	// ObserveAddr is the address to bind the metrics/pprof server.
	// Empty string disables the observability server.
	// Default: ":7702"
	ObserveAddr string

	// Logger is used for structured logging. Defaults to slog.Default().
	Logger *slog.Logger

	// MaxOpenConns sets the connection pool size for ModeStandard.
	// Ignored for ModeEmbedded. Default: 10.
	MaxOpenConns int

	// ConnectTimeout is the maximum time to wait for backend connection.
	// Default: 5s.
	ConnectTimeout time.Duration

	// DataDir is the data directory for ModeEmbedded persistent storage.
	// If empty, ModeEmbedded uses in-memory stores (no persistence).
	DataDir string

	// Extractor is an optional entity/relation extractor for IngestText.
	Extractor extract.Extractor

	// LLMProvider is an optional LLM provider for extraction and compaction.
	LLMProvider extract.Provider

	// Embedder is an optional auto-embedding provider. When set, Write()
	// will auto-embed content if no vector is provided, and Retrieve()
	// will auto-embed text queries.
	Embedder embedding.Embedder

	// EmbedModel identifies the embedding model for auto-embedded vectors.
	// Stored on nodes for provenance tracking.
	EmbedModel string

	// DedupWrites enables content fingerprint deduplication for all writes.
	// Default false preserves the historic behavior that each admitted Write
	// creates a node unless the individual WriteRequest opts in.
	DedupWrites bool

	// QdrantAddr is the Qdrant gRPC address for ModeScaled.
	// Example: "localhost:6334"
	QdrantAddr string

	// RedisAddr is the Redis address for ModeScaled.
	// Example: "localhost:6379"
	RedisAddr string

	// VectorDimensions is the embedding vector dimensionality for ModeScaled.
	// Required when using Qdrant. Default: 1536.
	VectorDimensions int
}

func (o *Options) withDefaults() Options {
	if o.Mode == "" {
		o.Mode = ModeEmbedded
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.MaxOpenConns == 0 {
		o.MaxOpenConns = 10
	}
	if o.ConnectTimeout == 0 {
		o.ConnectTimeout = 5 * time.Second
	}
	return *o
}

// closer is something that can be closed on shutdown.
type closer interface {
	Close() error
}

// DB is a contextdb connection handle. It is safe for concurrent use.
// Create one with [Open] and share it across your application — do not
// create a new DB per request.
type DB struct {
	opts    Options
	graph   store.GraphStore
	vecs    store.VectorIndex
	kv      store.KVStore
	log     store.EventLog
	metrics *observe.Metrics
	reg     *observe.Registry
	logger  *slog.Logger

	mu         sync.RWMutex
	namespaces map[string]*NamespaceHandle
	closed     bool
	closers    []closer // resources to close on shutdown
}

// Open opens a contextdb connection with the given options.
// It is analogous to sql.Open — returns immediately; the actual
// backend connection is established lazily on first use.
func Open(opts Options) (*DB, error) {
	opts = opts.withDefaults()

	reg := observe.NewRegistry()
	metrics := observe.NewMetrics(reg)

	db := &DB{
		opts:       opts,
		metrics:    metrics,
		reg:        reg,
		logger:     opts.Logger,
		namespaces: make(map[string]*NamespaceHandle),
	}

	if err := db.connect(); err != nil {
		return nil, fmt.Errorf("contextdb open: %w", err)
	}

	return db, nil
}

// MustOpen is like Open but panics on error. Useful in main() and tests.
func MustOpen(opts Options) *DB {
	db, err := Open(opts)
	if err != nil {
		panic("contextdb: " + err.Error())
	}
	return db
}

// connect initialises the storage backends based on the selected mode.
func (db *DB) connect() error {
	switch db.opts.Mode {
	case ModeEmbedded:
		if db.opts.DataDir != "" {
			return db.connectBadger()
		}
		db.graph = memstore.NewGraphStore()
		db.vecs = memstore.NewVectorIndex()
		db.kv = memstore.NewKVStore()
		db.log = memstore.NewEventLog()
		db.logger.Info("contextdb connected", "mode", "embedded", "storage", "memory")
		return nil

	case ModeStandard:
		if db.opts.DSN != "" {
			return db.connectPostgres()
		}
		db.logger.Warn("ModeStandard: no DSN provided, falling back to embedded")
		db.graph = memstore.NewGraphStore()
		db.vecs = memstore.NewVectorIndex()
		db.kv = memstore.NewKVStore()
		db.log = memstore.NewEventLog()
		return nil

	case ModeRemote:
		return db.connectRemote()

	case ModeScaled:
		// ModeScaled uses Postgres for graph, but falls back to embedded
		// for vector/KV/log when Qdrant/Redis are not compiled in (requires
		// integration build tag). The connectScaled method is provided in
		// a separate file with the integration build tag.
		db.logger.Warn("ModeScaled: Qdrant/Redis require integration build tag, falling back to standard graph + embedded stores")
		if db.opts.DSN != "" {
			if err := db.connectPostgres(); err != nil {
				return err
			}
		} else {
			db.graph = memstore.NewGraphStore()
			db.vecs = memstore.NewVectorIndex()
			db.kv = memstore.NewKVStore()
			db.log = memstore.NewEventLog()
		}
		return nil

	default:
		return fmt.Errorf("unknown mode: %q", db.opts.Mode)
	}
}

func (db *DB) connectBadger() error {
	bdb, err := badgerstore.Open(db.opts.DataDir)
	if err != nil {
		return fmt.Errorf("badger open: %w", err)
	}
	db.closers = append(db.closers, bdb)

	inner := bdb.Inner()
	db.graph = badgerstore.NewGraphStore(inner)
	vi := badgerstore.NewVectorIndex(inner, badgerstore.HNSWConfig{})
	if err := vi.Load(); err != nil {
		bdb.Close()
		return fmt.Errorf("badger load vectors: %w", err)
	}
	db.vecs = vi
	db.kv = badgerstore.NewKVStore(inner)
	db.log = badgerstore.NewEventLog(inner)
	db.logger.Info("contextdb connected", "mode", "embedded", "storage", "badger", "dir", db.opts.DataDir)
	return nil
}

func (db *DB) connectRemote() error {
	if db.opts.Addr == "" {
		return fmt.Errorf("ModeRemote: Addr is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), db.opts.ConnectTimeout)
	defer cancel()

	rc, err := remotestore.NewClient(ctx, db.opts.Addr)
	if err != nil {
		return fmt.Errorf("remote connect: %w", err)
	}
	db.closers = append(db.closers, rc)
	db.graph = rc.Graph()
	db.vecs = rc.Vectors()
	db.kv = rc.KV()
	db.log = rc.EventLog()
	db.logger.Info("contextdb connected", "mode", "remote", "addr", db.opts.Addr)
	return nil
}

func (db *DB) connectPostgres() error {
	ctx, cancel := context.WithTimeout(context.Background(), db.opts.ConnectTimeout)
	defer cancel()

	pool, err := pgstore.NewPool(ctx, db.opts.DSN, db.opts.MaxOpenConns)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	db.closers = append(db.closers, pool)

	migrator := pgstore.NewMigrator(pool.Inner())
	if err := migrator.Up(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("postgres migrate: %w", err)
	}

	inner := pool.Inner()
	db.graph = pgstore.NewGraphStore(inner)
	db.vecs = pgstore.NewVectorIndex(inner)
	db.kv = pgstore.NewKVStore(inner)
	db.log = pgstore.NewEventLog(inner)
	db.logger.Info("contextdb connected", "mode", "standard", "storage", "postgres")
	return nil
}

// Registry returns the observability registry for use by the server layer.
func (db *DB) Registry() *observe.Registry {
	return db.reg
}

// Stores returns the underlying store implementations. Useful for server
// layer and tests that need direct store access.
func (db *DB) Stores() (store.GraphStore, store.VectorIndex, store.KVStore, store.EventLog) {
	return db.graph, db.vecs, db.kv, db.log
}

// ExportSnapshot writes a namespace snapshot as NDJSON.
func (db *DB) ExportSnapshot(ctx context.Context, namespace string, w io.Writer) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return fmt.Errorf("contextdb: connection closed")
	}
	return snapshot.NewExporter(db.graph).Export(ctx, namespace, w)
}

// ExportSnapshotFromSeeds writes a seeded namespace snapshot as NDJSON.
func (db *DB) ExportSnapshotFromSeeds(ctx context.Context, namespace string, seedIDs []uuid.UUID, maxDepth int, w io.Writer) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return fmt.Errorf("contextdb: connection closed")
	}
	return snapshot.NewExporter(db.graph).ExportFromSeeds(ctx, namespace, seedIDs, maxDepth, w)
}

// ImportSnapshot imports an NDJSON snapshot into namespace.
func (db *DB) ImportSnapshot(ctx context.Context, namespace string, r io.Reader) error {
	_, err := db.ImportSnapshotReport(ctx, namespace, r)
	return err
}

// ImportSnapshotReport imports an NDJSON snapshot and returns processed counts.
func (db *DB) ImportSnapshotReport(ctx context.Context, namespace string, r io.Reader) (SnapshotReport, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return SnapshotReport{}, fmt.Errorf("contextdb: connection closed")
	}
	report, err := snapshot.NewImporter(db.graph, db.vecs).ImportWithReport(ctx, namespace, r)
	return snapshotReport(report, false), err
}

// ValidateSnapshot verifies an NDJSON snapshot without writing to the DB.
func (db *DB) ValidateSnapshot(ctx context.Context, namespace string, r io.Reader) error {
	_, err := db.ValidateSnapshotReport(ctx, namespace, r)
	return err
}

// ValidateSnapshotReport verifies an NDJSON snapshot without writing and returns processed counts.
func (db *DB) ValidateSnapshotReport(ctx context.Context, namespace string, r io.Reader) (SnapshotReport, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return SnapshotReport{}, fmt.Errorf("contextdb: connection closed")
	}
	report, err := snapshot.NewImporter(db.graph, db.vecs).ValidateWithReport(ctx, namespace, r)
	return snapshotReport(report, true), err
}

func snapshotReport(report snapshot.ImportReport, dryRun bool) SnapshotReport {
	return SnapshotReport{
		Namespace:          report.Namespace,
		DryRun:             dryRun,
		Lines:              report.Lines,
		Nodes:              report.Nodes,
		Edges:              report.Edges,
		Sources:            report.Sources,
		Vectors:            report.Vectors,
		NamespaceOverrides: report.NamespaceOverrides,
		NewNodes:           report.NewNodes,
		ChangedNodes:       report.ChangedNodes,
		UnchangedNodes:     report.UnchangedNodes,
	}
}

// Ping verifies the connection is still alive. Analogous to sql.DB.Ping.
func (db *DB) Ping(ctx context.Context) error {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return fmt.Errorf("contextdb: connection closed")
	}
	return nil
}

// Close releases all resources held by the DB.
// After Close, the DB is unusable. Analogous to sql.DB.Close.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil
	}
	db.closed = true
	for i := len(db.closers) - 1; i >= 0; i-- {
		_ = db.closers[i].Close()
	}
	db.logger.Info("contextdb closed")
	return nil
}

// Stats returns connection pool and metric statistics.
// Analogous to sql.DB.Stats.
func (db *DB) Stats() DBStats {
	snap := db.metrics.RetrievalLatency.Snapshot()
	return DBStats{
		Mode:            db.opts.Mode,
		RetrievalTotal:  db.metrics.RetrievalTotal.Value(),
		RetrievalErrors: db.metrics.RetrievalErrors.Value(),
		IngestTotal:     db.metrics.IngestTotal.Value(),
		IngestAdmitted:  db.metrics.IngestAdmitted.Value(),
		IngestRejected:  db.metrics.IngestRejected.Value(),
		LatencyP50Us:    snap.P(50),
		LatencyP95Us:    snap.P(95),
		LatencyMeanUs:   snap.Mean(),
	}
}

// DBStats holds runtime statistics for the DB.
type DBStats struct {
	Mode            Mode
	RetrievalTotal  int64
	RetrievalErrors int64
	IngestTotal     int64
	IngestAdmitted  int64
	IngestRejected  int64
	LatencyP50Us    float64
	LatencyP95Us    float64
	LatencyMeanUs   float64
}

// SnapshotReport summarizes records processed during snapshot import.
type SnapshotReport struct {
	Namespace          string `json:"namespace"`
	DryRun             bool   `json:"dry_run"`
	Lines              int    `json:"lines"`
	Nodes              int    `json:"nodes"`
	Edges              int    `json:"edges"`
	Sources            int    `json:"sources"`
	Vectors            int    `json:"vectors"`
	NamespaceOverrides int    `json:"namespace_overrides"`
	NewNodes           int    `json:"new_nodes"`
	ChangedNodes       int    `json:"changed_nodes"`
	UnchangedNodes     int    `json:"unchanged_nodes"`
}

// Namespace returns a handle for the named namespace, creating it if it
// does not exist. The mode argument sets the scoring defaults for this
// namespace. Subsequent calls with the same name return the same handle.
func (db *DB) Namespace(name string, mode namespace.Mode) *NamespaceHandle {
	db.mu.Lock()
	defer db.mu.Unlock()

	if h, ok := db.namespaces[name]; ok {
		return h
	}

	cfg := namespace.Defaults(name, mode)
	h := &NamespaceHandle{
		db:     db,
		cfg:    cfg,
		engine: &retrieval.Engine{Graph: db.graph, Vectors: db.vecs, KV: db.kv},
	}
	db.namespaces[name] = h
	db.metrics.ActiveNamespaces.Set(float64(len(db.namespaces)))
	db.logger.Info("namespace opened", "name", name, "mode", string(mode))
	return h
}

// ─── NamespaceHandle ─────────────────────────────────────────────────────────

// NamespaceHandle is scoped to a single namespace. All reads and writes
// through this handle are isolated from other namespaces.
type NamespaceHandle struct {
	db     *DB
	cfg    namespace.Config
	engine *retrieval.Engine
}

// ── Write path ────────────────────────────────────────────────────────────────

// WriteRequest describes a single write operation.
type WriteRequest struct {
	// Content is the raw text of the claim, memory, or fact.
	Content string

	// SourceID is the external identifier of the asserting source.
	// Used to look up or create a Source record and apply its credibility.
	SourceID string

	// Labels are caller-defined node labels (e.g. "Claim", "Skill", "Episode").
	Labels []string

	// Properties are arbitrary key-value metadata stored on the node.
	Properties map[string]any

	// Vector is the pre-computed embedding. If nil, the node is stored
	// without a vector and will not appear in ANN search results.
	Vector []float32

	// ModelID identifies the embedding model that produced Vector.
	ModelID string

	// Confidence is the initial confidence score [0,1].
	// If 0, defaults to the source's effective credibility.
	Confidence float64

	// ValidFrom is when the fact became true in the world.
	// Defaults to time.Now() if zero.
	ValidFrom time.Time

	// MemType sets the memory type for decay rate selection.
	// Only meaningful for agent-memory namespaces.
	MemType core.MemoryType

	// DependsOn lists node IDs that must be written before this request.
	// Used by WriteBatchOrdered to determine write order. Each UUID should
	// match a "node_id" property on another request in the same batch.
	// Dependencies on IDs outside the batch are silently ignored.
	DependsOn []uuid.UUID

	// SkipDedup bypasses content fingerprint deduplication. Use this when
	// intentionally re-ingesting the same text as a distinct claim.
	SkipDedup bool

	// Dedup opts this write into content fingerprint deduplication even when
	// Options.DedupWrites is false.
	Dedup bool
}

// WriteResult describes the outcome of a write operation.
type WriteResult struct {
	// NodeID is the ID of the written node, or the existing node ID if
	// the write was rejected as a near-duplicate.
	NodeID uuid.UUID

	// Admitted indicates whether the node was written to the graph.
	// False means the admission gate rejected it (see Reason).
	Admitted bool

	// Reason explains a rejection.
	Reason string

	// ConflictIDs are IDs of nodes this write contradicts.
	ConflictIDs []uuid.UUID
}

// FeedbackResult describes the result of a claim or memory feedback operation.
type FeedbackResult struct {
	NodeID            uuid.UUID
	Action            string
	Confidence        float64
	Utility           float64
	SourceID          string
	SourceCredibility float64
	Reason            string
}

// FeedbackEvent is an auditable record of validate/refute/useful/stale feedback.
type FeedbackEvent struct {
	EventID           uuid.UUID `json:"event_id,omitempty"`
	Namespace         string    `json:"namespace"`
	NodeID            uuid.UUID `json:"node_id"`
	NodeVersion       uint64    `json:"node_version,omitempty"`
	Action            string    `json:"action"`
	Confidence        float64   `json:"confidence"`
	Utility           float64   `json:"utility"`
	SourceID          string    `json:"source_id,omitempty"`
	SourceCredibility float64   `json:"source_credibility,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	Quality           int       `json:"quality,omitempty"`
	TxTime            time.Time `json:"tx_time"`
}

// SourceTrustPoint records one observed source credibility value over time.
type SourceTrustPoint struct {
	SourceID          string    `json:"source_id"`
	NodeID            uuid.UUID `json:"node_id"`
	Action            string    `json:"action"`
	SourceCredibility float64   `json:"source_credibility"`
	Reason            string    `json:"reason,omitempty"`
	TxTime            time.Time `json:"tx_time"`
}

// ReviewQueueRequest configures claim review queue generation.
type ReviewQueueRequest struct {
	After                           time.Time
	Now                             time.Time
	LowConfidenceThreshold          float64
	SourceTrustThreshold            float64
	SourceTrustDropThreshold        float64
	SourceRefutationThreshold       int
	EscalationAfter                 time.Duration
	SourceAnomalyEscalationPriority float64
	Types                           []string
	SourceID                        string
	Status                          string
	Owner                           string
	Limit                           int
}

// ReviewDecisionRequest records durable workflow state for a derived review task.
type ReviewDecisionRequest struct {
	ReviewID  string
	Status    string
	Owner     string
	Decision  string
	Note      string
	RecheckAt time.Time
}

// ReviewDecision is an append-only workflow event attached to a derived review item.
type ReviewDecision struct {
	EventID   uuid.UUID `json:"event_id,omitempty"`
	Namespace string    `json:"namespace"`
	ReviewID  string    `json:"review_id"`
	Status    string    `json:"status"`
	Owner     string    `json:"owner,omitempty"`
	Decision  string    `json:"decision,omitempty"`
	Note      string    `json:"note,omitempty"`
	RecheckAt time.Time `json:"recheck_at,omitempty"`
	TxTime    time.Time `json:"tx_time"`
}

// ReviewItem is a derived operator task for claims that need attention.
type ReviewItem struct {
	ID                 string      `json:"id"`
	Type               string      `json:"type"`
	Priority           float64     `json:"priority"`
	Reason             string      `json:"reason"`
	NodeID             uuid.UUID   `json:"node_id,omitempty"`
	NodeIDs            []uuid.UUID `json:"node_ids,omitempty"`
	SourceID           string      `json:"source_id,omitempty"`
	Action             string      `json:"action,omitempty"`
	Text               string      `json:"text,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	Suggested          string      `json:"suggested_action"`
	Confidence         float64     `json:"confidence,omitempty"`
	Status             string      `json:"status,omitempty"`
	Owner              string      `json:"owner,omitempty"`
	Decision           string      `json:"decision,omitempty"`
	Note               string      `json:"note,omitempty"`
	RecheckAt          time.Time   `json:"recheck_at,omitempty"`
	ReviewedAt         time.Time   `json:"reviewed_at,omitempty"`
	Escalated          bool        `json:"escalated,omitempty"`
	EscalationLevel    string      `json:"escalation_level,omitempty"`
	EscalationReason   string      `json:"escalation_reason,omitempty"`
	EscalationAgeHours float64     `json:"escalation_age_hours,omitempty"`
}

// ReviewEscalationDigest summarizes escalated review queue items for dashboards.
type ReviewEscalationDigest struct {
	EventID         uuid.UUID               `json:"event_id,omitempty"`
	Namespace       string                  `json:"namespace,omitempty"`
	GeneratedAt     time.Time               `json:"generated_at"`
	EscalationAfter time.Duration           `json:"escalation_after,omitempty"`
	Note            string                  `json:"note,omitempty"`
	TotalEscalated  int                     `json:"total_escalated"`
	Groups          []ReviewEscalationGroup `json:"groups"`
}

// ReviewEscalationGroup is one owner/source/type/severity bucket in a digest.
type ReviewEscalationGroup struct {
	Owner           string   `json:"owner"`
	SourceID        string   `json:"source_id,omitempty"`
	Type            string   `json:"type"`
	EscalationLevel string   `json:"escalation_level"`
	Count           int      `json:"count"`
	MaxPriority     float64  `json:"max_priority"`
	MaxAgeHours     float64  `json:"max_age_hours"`
	ReviewIDs       []string `json:"review_ids,omitempty"`
}

// ReviewHandoffRequest filters saved escalation digest snapshots for handoff feeds.
type ReviewHandoffRequest struct {
	After           time.Time
	Owner           string
	EscalationLevel string
	Limit           int
}

// ReviewHandoffWebhookRequest configures dry-run webhook delivery plans for saved handoff feeds.
type ReviewHandoffWebhookRequest struct {
	ReviewHandoffRequest
	TargetURL   string
	Secret      string
	MaxAttempts int
	Now         time.Time
	Execute     bool
	Timeout     time.Duration
	HTTPClient  *http.Client
}

// ReviewHandoffRetryRequest configures explicit retry execution for one failed handoff delivery.
type ReviewHandoffRetryRequest struct {
	After         time.Time
	DigestEventID uuid.UUID
	TargetURL     string
	Secret        string
	MaxAttempts   int
	Now           time.Time
	Execute       bool
	Timeout       time.Duration
	HTTPClient    *http.Client
}

// ReviewHandoffWebhookDelivery describes one dry-run webhook delivery that would be sent.
type ReviewHandoffWebhookDelivery struct {
	TargetURL       string                  `json:"target_url"`
	Method          string                  `json:"method"`
	DryRun          bool                    `json:"dry_run"`
	EventID         uuid.UUID               `json:"event_id"`
	Namespace       string                  `json:"namespace,omitempty"`
	GeneratedAt     time.Time               `json:"generated_at"`
	PlannedAt       time.Time               `json:"planned_at"`
	Owner           string                  `json:"owner,omitempty"`
	EscalationLevel string                  `json:"escalation_level,omitempty"`
	TotalEscalated  int                     `json:"total_escalated"`
	Groups          []ReviewEscalationGroup `json:"groups"`
	Payload         json.RawMessage         `json:"payload"`
	PayloadSHA256   string                  `json:"payload_sha256"`
	Signature       string                  `json:"signature,omitempty"`
	Headers         map[string]string       `json:"headers"`
	Attempt         int                     `json:"attempt"`
	MaxAttempts     int                     `json:"max_attempts"`
	NextRetryAfter  time.Duration           `json:"next_retry_after,omitempty"`
	Executed        bool                    `json:"executed,omitempty"`
	StatusCode      int                     `json:"status_code,omitempty"`
	ResponseBody    string                  `json:"response_body,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

// ReviewHandoffDeliveryReceipt is an append-only audit record for executed webhook delivery.
type ReviewHandoffDeliveryReceipt struct {
	ReceiptID       uuid.UUID `json:"receipt_id,omitempty"`
	DigestEventID   uuid.UUID `json:"digest_event_id"`
	Namespace       string    `json:"namespace,omitempty"`
	TargetURL       string    `json:"target_url"`
	DeliveredAt     time.Time `json:"delivered_at"`
	Owner           string    `json:"owner,omitempty"`
	EscalationLevel string    `json:"escalation_level,omitempty"`
	Success         bool      `json:"success"`
	StatusCode      int       `json:"status_code,omitempty"`
	PayloadSHA256   string    `json:"payload_sha256"`
	ResponseSHA256  string    `json:"response_sha256,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// ReviewHandoffRetryCandidate groups failed delivery receipts that may need retry.
type ReviewHandoffRetryCandidate struct {
	DigestEventID   uuid.UUID `json:"digest_event_id"`
	TargetURL       string    `json:"target_url"`
	LastReceiptID   uuid.UUID `json:"last_receipt_id"`
	LastAttemptAt   time.Time `json:"last_attempt_at"`
	Owner           string    `json:"owner,omitempty"`
	EscalationLevel string    `json:"escalation_level,omitempty"`
	Attempts        int       `json:"attempts"`
	LastStatusCode  int       `json:"last_status_code,omitempty"`
	PayloadSHA256   string    `json:"payload_sha256"`
	LastError       string    `json:"last_error,omitempty"`
}

// ReviewHandoffRetryRecommendation adds dry-run retry pacing guidance to a failed delivery candidate.
type ReviewHandoffRetryRecommendation struct {
	ReviewHandoffRetryCandidate
	RecommendedAfter time.Time `json:"recommended_after"`
	DelaySeconds     int       `json:"delay_seconds"`
	Ready            bool      `json:"ready"`
	Reason           string    `json:"reason"`
}

// ReviewHandoffRetryStatusFamilyCount summarizes retry recommendations by HTTP status family.
type ReviewHandoffRetryStatusFamilyCount struct {
	Family string `json:"family"`
	Count  int    `json:"count"`
}

// ReviewHandoffRetryFatigueSummary groups unresolved retry pressure by target URL.
type ReviewHandoffRetryFatigueSummary struct {
	TargetURL      string                                `json:"target_url"`
	Candidates     int                                   `json:"candidates"`
	TotalAttempts  int                                   `json:"total_attempts"`
	Ready          int                                   `json:"ready"`
	Waiting        int                                   `json:"waiting"`
	StatusFamilies []ReviewHandoffRetryStatusFamilyCount `json:"status_families"`
	LastStatusCode int                                   `json:"last_status_code,omitempty"`
	LastError      string                                `json:"last_error,omitempty"`
	LastAttemptAt  time.Time                             `json:"last_attempt_at,omitempty"`
}

// ReviewHandoffRetryFatigueMarkdown renders endpoint fatigue summaries for handoffs.
func ReviewHandoffRetryFatigueMarkdown(summaries []ReviewHandoffRetryFatigueSummary) string {
	var b strings.Builder
	b.WriteString("# Review Handoff Retry Fatigue\n\n")
	if len(summaries) == 0 {
		b.WriteString("No unresolved retry fatigue.\n")
		return b.String()
	}
	top := summaries[0]
	fmt.Fprintf(&b, "Top failing endpoint: `%s` with %d unresolved candidate(s) and %d total attempt(s).\n\n", markdownInline(top.TargetURL), top.Candidates, top.TotalAttempts)
	b.WriteString("| Target URL | Candidates | Attempts | Ready | Waiting | Status Families | Latest Failure |\n")
	b.WriteString("|:-----------|-----------:|---------:|------:|--------:|:----------------|:---------------|\n")
	for _, summary := range summaries {
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %s | %s |\n",
			markdownTable(summary.TargetURL),
			summary.Candidates,
			summary.TotalAttempts,
			summary.Ready,
			summary.Waiting,
			markdownTable(formatRetryStatusFamilies(summary.StatusFamilies)),
			markdownTable(formatRetryLatestFailure(summary)),
		)
	}
	return b.String()
}

// GapRequest configures knowledge-gap detection for a namespace.
type GapRequest struct {
	TopK       int
	MinGapSize float64
	MaxGaps    int
}

// AcquisitionPlanRequest configures knowledge acquisition planning.
type AcquisitionPlanRequest struct {
	TopK       int
	MinGapSize float64
	MaxGaps    int
	Budget     int
}

// AcquisitionTask is a suggested research, crawl, or verification task.
type AcquisitionTask struct {
	ID             string      `json:"id"`
	Type           string      `json:"type"`
	Priority       float64     `json:"priority"`
	Description    string      `json:"description"`
	Prompt         string      `json:"prompt"`
	RelatedNodeIDs []uuid.UUID `json:"related_node_ids,omitempty"`
	NearestTopics  []string    `json:"nearest_topics,omitempty"`
}

// AcquisitionPlan turns gaps and weak claims into concrete acquisition tasks.
type AcquisitionPlan struct {
	Namespace     string            `json:"namespace"`
	CoverageScore float64           `json:"coverage_score"`
	TotalNodes    int               `json:"total_nodes"`
	Tasks         []AcquisitionTask `json:"tasks"`
}

// Write ingests a new claim, memory, or fact into the namespace.
// It runs through the admission gate (credibility floor, near-duplicate
// check, novelty threshold) before writing.
func (h *NamespaceHandle) Write(ctx context.Context, req WriteRequest) (WriteResult, error) {
	start := time.Now()
	h.db.metrics.IngestTotal.Inc()

	fingerprint := ""
	if (h.db.opts.DedupWrites || req.Dedup) && !req.SkipDedup && req.Content != "" {
		fingerprint = core.ContentFingerprint(req.Content)
		if fingerprint != "" {
			existing, err := h.db.graph.GetNodeByFingerprint(ctx, h.cfg.ID, fingerprint)
			if err != nil {
				return WriteResult{}, fmt.Errorf("write: fingerprint lookup: %w", err)
			}
			if existing != nil {
				if err := h.db.graph.TouchNode(ctx, h.cfg.ID, existing.ID, time.Now()); err != nil {
					return WriteResult{}, fmt.Errorf("write: touch deduplicated node: %w", err)
				}
				h.db.metrics.AdmissionDuplicateSkipped.Inc()
				h.db.metrics.IngestAdmitted.Inc()
				h.db.metrics.IngestLatency.ObserveDuration(time.Since(start))
				h.db.logger.Debug("write deduplicated",
					"namespace", h.cfg.ID,
					"node_id", existing.ID,
					"source", req.SourceID)
				return WriteResult{
					NodeID:   existing.ID,
					Admitted: true,
					Reason:   "deduplicated",
				}, nil
			}
		}
	}

	// Auto-embed if no vector provided and embedder is configured
	if len(req.Vector) == 0 && req.Content != "" && h.db.opts.Embedder != nil {
		vecs, err := h.db.opts.Embedder.Embed(ctx, []string{req.Content})
		if err != nil {
			return WriteResult{}, fmt.Errorf("write: auto-embed: %w", err)
		}
		if len(vecs) > 0 {
			req.Vector = vecs[0]
			if req.ModelID == "" {
				req.ModelID = h.db.opts.EmbedModel
			}
		}
	}

	// Resolve or create source
	src, err := h.resolveSource(ctx, req.SourceID)
	if err != nil {
		return WriteResult{}, fmt.Errorf("write: resolve source: %w", err)
	}

	// Set ValidFrom
	validFrom := req.ValidFrom
	if validFrom.IsZero() {
		validFrom = time.Now()
	}

	// Set confidence
	confidence := req.Confidence
	if confidence == 0 {
		confidence = src.EffectiveCredibility()
	}

	// Build candidate node
	props := req.Properties
	if props == nil {
		props = make(map[string]any)
	}
	if req.Content != "" {
		props["text"] = req.Content
	}
	if req.SourceID != "" {
		props["source_id"] = req.SourceID
	}

	candidate := core.Node{
		ID:          uuid.New(),
		Namespace:   h.cfg.ID,
		Labels:      req.Labels,
		Properties:  props,
		Vector:      req.Vector,
		ModelID:     req.ModelID,
		Fingerprint: fingerprint,
		Confidence:  confidence,
		ValidFrom:   validFrom,
		TxTime:      time.Now(),
	}

	// Quick ANN scan for near-duplicate detection
	var nearest []core.ScoredNode
	if len(req.Vector) > 0 {
		nearest, err = h.db.vecs.Search(ctx, store.VectorQuery{
			Namespace: h.cfg.ID,
			Vector:    req.Vector,
			TopK:      5,
			AsOf:      time.Now(),
		})
		if err != nil {
			return WriteResult{}, fmt.Errorf("write: near-duplicate scan: %w", err)
		}
	}

	// Admission gate
	decision := ingest.Admit(ingest.AdmitRequest{
		Candidate:         candidate,
		Source:            src,
		NearestNeighbours: nearest,
		Threshold:         h.cfg.AdmitThreshold,
	})

	if !decision.Admit {
		h.db.metrics.IngestRejected.Inc()
		switch {
		case containsStr(decision.Reason, "credibility below floor"):
			h.db.metrics.AdmissionTrollRejected.Inc()
		case containsStr(decision.Reason, "near-duplicate"):
			h.db.metrics.AdmissionDuplicateSkipped.Inc()
		default:
			h.db.metrics.AdmissionThresholdFailed.Inc()
		}
		h.db.logger.Debug("write rejected",
			"namespace", h.cfg.ID,
			"source", req.SourceID,
			"reason", decision.Reason)
		return WriteResult{Admitted: false, Reason: decision.Reason}, nil
	}

	// Apply confidence multiplier
	candidate.Confidence = confidence * decision.ConfidenceMultiplier

	// Write to graph
	t0 := time.Now()
	if err := h.db.graph.UpsertNode(ctx, candidate); err != nil {
		return WriteResult{}, fmt.Errorf("write: upsert node: %w", err)
	}
	h.db.metrics.GraphUpsertLatency.ObserveDuration(time.Since(t0))
	h.db.metrics.GraphUpsertTotal.Inc()
	h.db.metrics.NodeCount.Add(1)

	// Register with vector index
	if len(req.Vector) > 0 {
		if reg, ok := h.db.vecs.(interface{ RegisterNode(core.Node) }); ok {
			reg.RegisterNode(candidate)
		}
		t1 := time.Now()
		nID := candidate.ID
		if err := h.db.vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: h.cfg.ID,
			NodeID:    &nID,
			Vector:    req.Vector,
			Text:      req.Content,
			ModelID:   req.ModelID,
			CreatedAt: time.Now(),
		}); err != nil {
			return WriteResult{}, fmt.Errorf("write: index vector: %w", err)
		}
		h.db.metrics.VectorIndexLatency.ObserveDuration(time.Since(t1))
		h.db.metrics.VectorIndexTotal.Inc()
	}

	h.db.metrics.IngestAdmitted.Inc()
	h.db.metrics.IngestLatency.ObserveDuration(time.Since(start))

	// Conflict detection (if we have nearest neighbours to check against)
	var conflictIDs []uuid.UUID
	if len(nearest) > 0 {
		detector := ingest.NewConflictDetector(h.db.graph, h.db.opts.LLMProvider)
		cResult, cErr := detector.Detect(ctx, candidate, nearest)
		if cErr != nil {
			h.db.logger.Warn("conflict detection failed", "error", cErr)
		} else {
			conflictIDs = cResult.ConflictIDs
		}
	}

	h.db.logger.Debug("write admitted",
		"namespace", h.cfg.ID,
		"node_id", candidate.ID,
		"source", req.SourceID,
		"confidence", candidate.Confidence,
		"conflicts", len(conflictIDs))

	return WriteResult{
		NodeID:      candidate.ID,
		Admitted:    true,
		ConflictIDs: conflictIDs,
	}, nil
}

// IngestText runs raw text through the extraction pipeline, producing nodes
// and edges automatically. Requires Options.Extractor to be set.
func (h *NamespaceHandle) IngestText(ctx context.Context, text, sourceID string) (*ingest.IngestResult, error) {
	if h.db.opts.Extractor == nil {
		return nil, fmt.Errorf("IngestText: no extractor configured")
	}

	pipeline := ingest.NewPipeline(
		h.db.opts.Extractor,
		h.db.graph,
		h.db.vecs,
		h.db.log,
		ingest.PipelineConfig{AdmitThreshold: h.cfg.AdmitThreshold},
	)

	return pipeline.Ingest(ctx, ingest.IngestRequest{
		Text:      text,
		Namespace: h.cfg.ID,
		SourceID:  sourceID,
	})
}

// ── Read path ─────────────────────────────────────────────────────────────────

// RetrieveRequest describes a single retrieval operation.
type RetrieveRequest struct {
	// Vector is the query embedding. If nil and Text is set with an
	// Embedder configured, the text will be auto-embedded.
	Vector []float32

	// Text is a natural-language query string. When an Embedder is
	// configured and Vector is nil, this text is auto-embedded to
	// produce the query vector.
	Text string

	// Vectors allows multi-vector queries. Results from all vectors
	// are fused together with the primary Vector.
	Vectors [][]float32

	// SeedIDs are known relevant node IDs for graph traversal.
	// Optional — if empty, only vector search is used.
	SeedIDs []uuid.UUID

	// TopK is the maximum number of results to return. Default: 10.
	TopK int

	// Labels restricts results to nodes carrying all specified labels.
	Labels []string

	// ScoreParams overrides the namespace default scoring strategy.
	// Zero value uses namespace defaults.
	ScoreParams core.ScoreParams

	// Strategy overrides the namespace default retrieval strategy.
	Strategy retrieval.HybridStrategy

	// AsOf pins retrieval to a historical time (temporal query).
	// Zero value = now.
	AsOf time.Time

	// ExcludeSourceIDs filters out nodes from specific sources.
	// Supports counterfactual queries: "what if source X didn't exist?"
	ExcludeSourceIDs []string
}

// Result is a single retrieval result with its score breakdown.
type Result struct {
	Node core.Node

	// Score is the composite retrieval score [0, 1].
	Score float64

	// Components expose why this node ranked where it did.
	SimilarityScore float64
	ConfidenceScore float64
	RecencyScore    float64
	UtilityScore    float64
	Breakdown       core.ScoreBreakdown

	// RetrievalSource indicates which path(s) found this node.
	RetrievalSource string
}

// ExplainRankRequest compares two nodes under the namespace scoring strategy.
type ExplainRankRequest struct {
	NodeID      uuid.UUID
	OtherNodeID uuid.UUID
	Text        string
	Vector      []float32
	ScoreParams core.ScoreParams
	AsOf        time.Time
	MaxDepth    int
}

// RankedNodeExplanation is one side of a rank comparison.
type RankedNodeExplanation struct {
	NodeID          uuid.UUID           `json:"node_id"`
	Text            string              `json:"text,omitempty"`
	Score           float64             `json:"score"`
	SimilarityScore float64             `json:"similarity_score"`
	ConfidenceScore float64             `json:"confidence_score"`
	RecencyScore    float64             `json:"recency_score"`
	UtilityScore    float64             `json:"utility_score"`
	ScoreBreakdown  core.ScoreBreakdown `json:"score_breakdown"`
	RetrievalSource string              `json:"retrieval_source"`
	Evidence        RankEvidence        `json:"evidence"`
}

// RankEvidence summarizes graph evidence connected to one ranked node.
type RankEvidence struct {
	CompoundConfidence float64            `json:"compound_confidence"`
	SupportCount       int                `json:"support_count"`
	Links              []RankEvidenceLink `json:"links"`
}

// RankEvidenceLink is one supporting edge in a rank explanation.
type RankEvidenceLink struct {
	NodeID     uuid.UUID `json:"node_id"`
	EdgeID     uuid.UUID `json:"edge_id"`
	EdgeWeight float64   `json:"edge_weight"`
	Confidence float64   `json:"confidence"`
	Text       string    `json:"text,omitempty"`
}

// RankFactorDelta explains how much one score component favored NodeID.
type RankFactorDelta struct {
	Factor            string  `json:"factor"`
	NodeContribution  float64 `json:"node_contribution"`
	OtherContribution float64 `json:"other_contribution"`
	Delta             float64 `json:"delta"`
}

// RankExplanation explains why one node ranks above another.
type RankExplanation struct {
	Node         RankedNodeExplanation `json:"node"`
	Other        RankedNodeExplanation `json:"other"`
	WinnerNodeID uuid.UUID             `json:"winner_node_id,omitempty"`
	LoserNodeID  uuid.UUID             `json:"loser_node_id,omitempty"`
	Margin       float64               `json:"margin"`
	Summary      string                `json:"summary"`
	Factors      []RankFactorDelta     `json:"factors"`
}

// Retrieve runs a hybrid retrieval query against the namespace.
func (h *NamespaceHandle) Retrieve(ctx context.Context, req RetrieveRequest) ([]Result, error) {
	start := time.Now()
	h.db.metrics.RetrievalTotal.Inc()

	// Auto-embed text query if no vector provided and embedder is configured
	if len(req.Vector) == 0 && req.Text != "" && h.db.opts.Embedder != nil {
		vecs, err := h.db.opts.Embedder.Embed(ctx, []string{req.Text})
		if err != nil {
			return nil, fmt.Errorf("retrieve: auto-embed query: %w", err)
		}
		if len(vecs) > 0 {
			req.Vector = vecs[0]
		}
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}

	params := req.ScoreParams
	if params == (core.ScoreParams{}) {
		params = h.cfg.ScoreParams
	}
	if req.AsOf.IsZero() {
		params.AsOf = time.Now()
	} else {
		params.AsOf = req.AsOf
	}

	strategy := req.Strategy
	if strategy == (retrieval.HybridStrategy{}) {
		strategy = retrieval.HybridStrategy{
			VectorWeight:  0.45,
			GraphWeight:   0.40,
			SessionWeight: 0.15,
			Traversal:     h.cfg.Traversal,
			MaxDepth:      h.cfg.MaxDepth,
		}
	}

	q := retrieval.Query{
		Namespace:        h.cfg.ID,
		Vector:           req.Vector,
		Vectors:          req.Vectors,
		QueryText:        req.Text,
		SeedIDs:          req.SeedIDs,
		TopK:             topK,
		Labels:           req.Labels,
		ExcludeSourceIDs: req.ExcludeSourceIDs,
		Strategy:         strategy,
		ScoreParams:      params,
	}

	scored, err := h.engine.Retrieve(ctx, q)
	if err != nil {
		h.db.metrics.RetrievalErrors.Inc()
		return nil, fmt.Errorf("retrieve: %w", err)
	}

	h.db.metrics.RetrievalLatency.ObserveDuration(time.Since(start))
	h.db.metrics.RetrievalResults.Set(float64(len(scored)))
	if len(scored) > 0 {
		h.db.metrics.RetrievalTopScore.Set(scored[0].Score)
	}

	results := make([]Result, len(scored))
	for i, sn := range scored {
		results[i] = Result{
			Node:            sn.Node,
			Score:           sn.Score,
			SimilarityScore: sn.SimilarityScore,
			ConfidenceScore: sn.ConfidenceScore,
			RecencyScore:    sn.RecencyScore,
			UtilityScore:    sn.UtilityScore,
			Breakdown:       sn.Breakdown,
			RetrievalSource: sn.RetrievalSource,
		}
		switch sn.RetrievalSource {
		case "vector":
			h.db.metrics.VectorHits.Inc()
		case "graph":
			h.db.metrics.GraphHits.Inc()
		default:
			h.db.metrics.FusedHits.Inc()
		}
	}

	return results, nil
}

// ExplainRank compares two nodes and returns the score factors that separate them.
func (h *NamespaceHandle) ExplainRank(ctx context.Context, req ExplainRankRequest) (*RankExplanation, error) {
	if req.NodeID == uuid.Nil || req.OtherNodeID == uuid.Nil {
		return nil, fmt.Errorf("explain rank: both node IDs are required")
	}
	if len(req.Vector) == 0 && req.Text != "" && h.db.opts.Embedder != nil {
		vecs, err := h.db.opts.Embedder.Embed(ctx, []string{req.Text})
		if err != nil {
			return nil, fmt.Errorf("explain rank: auto-embed query: %w", err)
		}
		if len(vecs) > 0 {
			req.Vector = vecs[0]
		}
	}

	left, err := h.db.graph.GetNode(ctx, h.cfg.ID, req.NodeID)
	if err != nil {
		return nil, fmt.Errorf("explain rank: get node: %w", err)
	}
	if left == nil {
		return nil, fmt.Errorf("explain rank: node %s not found", req.NodeID)
	}
	right, err := h.db.graph.GetNode(ctx, h.cfg.ID, req.OtherNodeID)
	if err != nil {
		return nil, fmt.Errorf("explain rank: get other node: %w", err)
	}
	if right == nil {
		return nil, fmt.Errorf("explain rank: node %s not found", req.OtherNodeID)
	}

	params := req.ScoreParams
	if params == (core.ScoreParams{}) {
		params = h.cfg.ScoreParams
	}
	if req.AsOf.IsZero() {
		params.AsOf = time.Now()
	} else {
		params.AsOf = req.AsOf
	}

	leftScored := scoreRankNode(*left, req.Vector, params)
	rightScored := scoreRankNode(*right, req.Vector, params)
	leftEvidence, err := h.rankEvidence(ctx, leftScored.Node.ID, req.MaxDepth)
	if err != nil {
		return nil, err
	}
	rightEvidence, err := h.rankEvidence(ctx, rightScored.Node.ID, req.MaxDepth)
	if err != nil {
		return nil, err
	}
	leftExplanation := newRankedNodeExplanation(leftScored)
	leftExplanation.Evidence = leftEvidence
	rightExplanation := newRankedNodeExplanation(rightScored)
	rightExplanation.Evidence = rightEvidence
	explanation := &RankExplanation{
		Node:    leftExplanation,
		Other:   rightExplanation,
		Margin:  leftScored.Score - rightScored.Score,
		Factors: rankFactorDeltas(leftScored.Breakdown, rightScored.Breakdown),
	}
	switch {
	case leftScored.Score > rightScored.Score:
		explanation.WinnerNodeID = leftScored.Node.ID
		explanation.LoserNodeID = rightScored.Node.ID
		explanation.Summary = rankSummary(leftScored, rightScored)
	case rightScored.Score > leftScored.Score:
		explanation.WinnerNodeID = rightScored.Node.ID
		explanation.LoserNodeID = leftScored.Node.ID
		explanation.Summary = rankSummary(rightScored, leftScored)
	default:
		explanation.Summary = "The nodes are tied under the current scoring inputs."
	}
	return explanation, nil
}

// ── Source helpers ─────────────────────────────────────────────────────────────

func (h *NamespaceHandle) resolveSource(ctx context.Context, externalID string) (core.Source, error) {
	if externalID == "" {
		return core.DefaultSource(h.cfg.ID, "anonymous"), nil
	}
	existing, err := h.db.graph.GetSourceByExternalID(ctx, h.cfg.ID, externalID)
	if err != nil {
		return core.Source{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	src := core.DefaultSource(h.cfg.ID, externalID)
	if err := h.db.graph.UpsertSource(ctx, src); err != nil {
		return core.Source{}, err
	}
	return src, nil
}

// LabelSource sets labels on a source to apply credibility overrides.
// Use "moderator" or "admin" for full trust, "troll" or "flagged" for floor.
func (h *NamespaceHandle) LabelSource(ctx context.Context, externalID string, labels []string) error {
	src, err := h.resolveSource(ctx, externalID)
	if err != nil {
		return err
	}
	src.Labels = labels
	return h.db.graph.UpsertSource(ctx, src)
}

// Consensus resolves the truth estimate for a claim node by aggregating all
// source assertions recorded against that claim ID. It uses a credibility-
// weighted vote (see ingest.MultiSourceConsensus). Domain-scoped credibility
// is applied automatically when the claim node carries a "domain" property or
// labels that can be used as a domain proxy.
func (h *NamespaceHandle) Consensus(ctx context.Context, claimID uuid.UUID) (*ingest.TruthEstimate, error) {
	resolver := ingest.NewConsensusResolver(h.db.graph, h.db.logger)
	est, err := resolver.ResolveTruth(ctx, claimID)
	if err != nil {
		return nil, fmt.Errorf("consensus: %w", err)
	}
	return &est, nil
}

// ValidateClaim records that a claim has been externally validated.
func (h *NamespaceHandle) ValidateClaim(ctx context.Context, nodeID uuid.UUID) (FeedbackResult, error) {
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:          "validated",
		confidenceDelta: 0.15,
		utilityDelta:    0.05,
		sourceValidated: ptrBool(true),
		quality:         5,
	})
}

// RefuteClaim records that a claim has been externally refuted.
func (h *NamespaceHandle) RefuteClaim(ctx context.Context, nodeID uuid.UUID, reason string) (FeedbackResult, error) {
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:          "refuted",
		confidenceSet:   ptrFloat64(0.05),
		utilityDelta:    -0.2,
		sourceValidated: ptrBool(false),
		quality:         1,
		reason:          reason,
	})
}

// MarkUseful records positive task-outcome feedback for an agent memory.
func (h *NamespaceHandle) MarkUseful(ctx context.Context, nodeID uuid.UUID, quality int) (FeedbackResult, error) {
	if quality == 0 {
		quality = 5
	}
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:       "useful",
		utilityDelta: 0.15,
		quality:      quality,
	})
}

// MarkStale records that a node is no longer useful as current context.
func (h *NamespaceHandle) MarkStale(ctx context.Context, nodeID uuid.UUID, reason string) (FeedbackResult, error) {
	return h.applyFeedback(ctx, nodeID, feedbackUpdate{
		action:          "stale",
		confidenceDelta: -0.1,
		utilityDelta:    -0.25,
		quality:         1,
		reason:          reason,
	})
}

// FeedbackEvents returns durable feedback audit events after the given time.
func (h *NamespaceHandle) FeedbackEvents(ctx context.Context, after time.Time) ([]FeedbackEvent, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("feedback events: %w", err)
	}
	out := make([]FeedbackEvent, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventFeedback {
			continue
		}
		var feedback FeedbackEvent
		if err := json.Unmarshal(event.Payload, &feedback); err != nil {
			return nil, fmt.Errorf("feedback events: decode %s: %w", event.ID, err)
		}
		feedback.EventID = event.ID
		if feedback.Namespace == "" {
			feedback.Namespace = event.Namespace
		}
		if feedback.TxTime.IsZero() {
			feedback.TxTime = event.TxTime
		}
		out = append(out, feedback)
	}
	return out, nil
}

// SourceTrustTimeline returns credibility observations for one source after the given time.
func (h *NamespaceHandle) SourceTrustTimeline(ctx context.Context, sourceID string, after time.Time) ([]SourceTrustPoint, error) {
	events, err := h.FeedbackEvents(ctx, after)
	if err != nil {
		return nil, err
	}
	out := make([]SourceTrustPoint, 0, len(events))
	for _, event := range events {
		if event.SourceID != sourceID || event.SourceCredibility == 0 {
			continue
		}
		out = append(out, SourceTrustPoint{
			SourceID:          event.SourceID,
			NodeID:            event.NodeID,
			Action:            event.Action,
			SourceCredibility: event.SourceCredibility,
			Reason:            event.Reason,
			TxTime:            event.TxTime,
		})
	}
	return out, nil
}

// RecordReviewDecision appends durable workflow state for a derived review task.
func (h *NamespaceHandle) RecordReviewDecision(ctx context.Context, req ReviewDecisionRequest) (ReviewDecision, error) {
	reviewID := strings.TrimSpace(req.ReviewID)
	if reviewID == "" {
		return ReviewDecision{}, fmt.Errorf("review decision: review_id is required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "open"
	}
	switch status {
	case "open", "assigned", "resolved", "snoozed":
	default:
		return ReviewDecision{}, fmt.Errorf("review decision: unsupported status %q", status)
	}
	if status == "snoozed" && req.RecheckAt.IsZero() {
		return ReviewDecision{}, fmt.Errorf("review decision: recheck_at is required for snoozed status")
	}

	decision := ReviewDecision{
		EventID:   uuid.New(),
		Namespace: h.cfg.ID,
		ReviewID:  reviewID,
		Status:    status,
		Owner:     strings.TrimSpace(req.Owner),
		Decision:  strings.TrimSpace(req.Decision),
		Note:      strings.TrimSpace(req.Note),
		RecheckAt: req.RecheckAt,
		TxTime:    time.Now(),
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return ReviewDecision{}, fmt.Errorf("review decision: marshal: %w", err)
	}
	event := store.Event{
		ID:        decision.EventID,
		Namespace: h.cfg.ID,
		Type:      store.EventReviewDecision,
		Payload:   payload,
		TxTime:    decision.TxTime,
	}
	if err := h.db.log.Append(ctx, event); err != nil {
		return ReviewDecision{}, fmt.Errorf("review decision: append event: %w", err)
	}
	return decision, nil
}

// ReviewDecisions returns durable review workflow events after the given time.
func (h *NamespaceHandle) ReviewDecisions(ctx context.Context, after time.Time) ([]ReviewDecision, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("review decisions: %w", err)
	}
	out := make([]ReviewDecision, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventReviewDecision {
			continue
		}
		var decision ReviewDecision
		if err := json.Unmarshal(event.Payload, &decision); err != nil {
			return nil, fmt.Errorf("review decisions: decode %s: %w", event.ID, err)
		}
		decision.EventID = event.ID
		if decision.Namespace == "" {
			decision.Namespace = event.Namespace
		}
		if decision.TxTime.IsZero() {
			decision.TxTime = event.TxTime
		}
		out = append(out, decision)
	}
	return out, nil
}

// ReviewQueue derives operator review tasks from feedback, low-confidence claims, and contradictions.
func (h *NamespaceHandle) ReviewQueue(ctx context.Context, req ReviewQueueRequest) ([]ReviewItem, error) {
	threshold := req.LowConfidenceThreshold
	if threshold == 0 {
		threshold = 0.35
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	items := make([]ReviewItem, 0)

	events, err := h.FeedbackEvents(ctx, req.After)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		switch event.Action {
		case "refuted", "stale":
			items = append(items, ReviewItem{
				ID:         fmt.Sprintf("feedback:%s", event.EventID),
				Type:       event.Action,
				Priority:   reviewFeedbackPriority(event),
				Reason:     event.Reason,
				NodeID:     event.NodeID,
				SourceID:   event.SourceID,
				Action:     event.Action,
				CreatedAt:  event.TxTime,
				Suggested:  reviewSuggestion(event.Action),
				Confidence: event.Confidence,
			})
		}
	}
	items = append(items, sourceTrustAnomalyItems(events, req)...)

	nodes, err := h.db.graph.ValidAt(ctx, h.cfg.ID, now, nil)
	if err != nil {
		return nil, fmt.Errorf("review queue: scan nodes: %w", err)
	}
	for _, node := range nodes {
		confidence := node.Confidence
		if confidence == 0 {
			confidence = 0.5
		}
		if confidence <= threshold {
			items = append(items, ReviewItem{
				ID:         "low_confidence:" + node.ID.String(),
				Type:       "low_confidence",
				Priority:   threshold - confidence + 0.2,
				Reason:     fmt.Sprintf("confidence %.2f is below %.2f", confidence, threshold),
				NodeID:     node.ID,
				SourceID:   nodeSourceID(node),
				Text:       core.NodeText(node),
				CreatedAt:  node.TxTime,
				Suggested:  "validate, refute, or attach stronger evidence",
				Confidence: confidence,
			})
		}
	}

	clusters, err := retrieval.FindConflictClusters(ctx, h.db.graph, h.cfg.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("review queue: conflicts: %w", err)
	}
	for _, cluster := range clusters {
		nodeIDs := make([]uuid.UUID, 0, len(cluster.Nodes))
		for _, node := range cluster.Nodes {
			nodeIDs = append(nodeIDs, node.ID)
		}
		items = append(items, ReviewItem{
			ID:        "conflict:" + reviewIDsKey(nodeIDs),
			Type:      "conflict",
			Priority:  0.7 + cluster.CredibilityGap,
			Reason:    fmt.Sprintf("%d claims are connected by contradiction edges", len(cluster.Nodes)),
			NodeIDs:   nodeIDs,
			CreatedAt: now,
			Suggested: "compare evidence and resolve the contradiction",
		})
	}

	decisions, err := h.ReviewDecisions(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	items = applyReviewDecisions(items, latestReviewDecisions(decisions), now)
	items = applyReviewEscalations(items, req, now)
	items = filterReviewItems(items, req)

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].Priority > items[j].Priority
	})
	if req.Limit > 0 && len(items) > req.Limit {
		items = items[:req.Limit]
	}
	return items, nil
}

// ReviewEscalationDigest groups escalated review queue items by owner, source, type, and level.
func (h *NamespaceHandle) ReviewEscalationDigest(ctx context.Context, req ReviewQueueRequest) (ReviewEscalationDigest, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	if req.EscalationAfter <= 0 {
		req.EscalationAfter = 72 * time.Hour
	}
	req.Now = now
	items, err := h.ReviewQueue(ctx, req)
	if err != nil {
		return ReviewEscalationDigest{}, err
	}
	digest := ReviewEscalationDigest{
		Namespace:       h.cfg.ID,
		GeneratedAt:     now,
		EscalationAfter: req.EscalationAfter,
		Groups:          []ReviewEscalationGroup{},
	}
	grouped := map[string]*ReviewEscalationGroup{}
	for _, item := range items {
		if !item.Escalated {
			continue
		}
		digest.TotalEscalated++
		owner := strings.TrimSpace(item.Owner)
		if owner == "" {
			owner = "unassigned"
		}
		sourceID := strings.TrimSpace(item.SourceID)
		if sourceID == "" {
			sourceID = "unsourced"
		}
		level := strings.TrimSpace(item.EscalationLevel)
		if level == "" {
			level = "escalated"
		}
		key := strings.Join([]string{owner, sourceID, item.Type, level}, "\x00")
		group := grouped[key]
		if group == nil {
			group = &ReviewEscalationGroup{
				Owner:           owner,
				SourceID:        sourceID,
				Type:            item.Type,
				EscalationLevel: level,
			}
			grouped[key] = group
		}
		group.Count++
		group.MaxPriority = maxFloat(group.MaxPriority, item.Priority)
		group.MaxAgeHours = maxFloat(group.MaxAgeHours, item.EscalationAgeHours)
		group.ReviewIDs = append(group.ReviewIDs, item.ID)
	}
	for _, group := range grouped {
		sort.Strings(group.ReviewIDs)
		digest.Groups = append(digest.Groups, *group)
	}
	sort.SliceStable(digest.Groups, func(i, j int) bool {
		left, right := digest.Groups[i], digest.Groups[j]
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		if left.MaxAgeHours != right.MaxAgeHours {
			return left.MaxAgeHours > right.MaxAgeHours
		}
		return strings.Join([]string{left.Owner, left.SourceID, left.Type, left.EscalationLevel}, "\x00") <
			strings.Join([]string{right.Owner, right.SourceID, right.Type, right.EscalationLevel}, "\x00")
	})
	return digest, nil
}

// RecordReviewEscalationDigest appends a durable digest snapshot for handoffs.
func (h *NamespaceHandle) RecordReviewEscalationDigest(ctx context.Context, req ReviewQueueRequest, note string) (ReviewEscalationDigest, error) {
	digest, err := h.ReviewEscalationDigest(ctx, req)
	if err != nil {
		return ReviewEscalationDigest{}, err
	}
	digest.EventID = uuid.New()
	digest.Namespace = h.cfg.ID
	digest.Note = strings.TrimSpace(note)
	payload, err := json.Marshal(digest)
	if err != nil {
		return ReviewEscalationDigest{}, fmt.Errorf("review escalation digest: marshal: %w", err)
	}
	event := store.Event{
		ID:        digest.EventID,
		Namespace: h.cfg.ID,
		Type:      store.EventReviewEscalationDigest,
		Payload:   payload,
		TxTime:    digest.GeneratedAt,
	}
	if err := h.db.log.Append(ctx, event); err != nil {
		return ReviewEscalationDigest{}, fmt.Errorf("review escalation digest: append event: %w", err)
	}
	return digest, nil
}

// ReviewEscalationDigests returns durable escalation digest snapshots after the given time.
func (h *NamespaceHandle) ReviewEscalationDigests(ctx context.Context, after time.Time) ([]ReviewEscalationDigest, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("review escalation digests: %w", err)
	}
	out := make([]ReviewEscalationDigest, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventReviewEscalationDigest {
			continue
		}
		var digest ReviewEscalationDigest
		if err := json.Unmarshal(event.Payload, &digest); err != nil {
			return nil, fmt.Errorf("review escalation digests: decode %s: %w", event.ID, err)
		}
		digest.EventID = event.ID
		if digest.Namespace == "" {
			digest.Namespace = event.Namespace
		}
		if digest.GeneratedAt.IsZero() {
			digest.GeneratedAt = event.TxTime
		}
		out = append(out, digest)
	}
	return out, nil
}

// ReviewHandoffs returns saved digest snapshots filtered for polling by owner or escalation level.
func (h *NamespaceHandle) ReviewHandoffs(ctx context.Context, req ReviewHandoffRequest) ([]ReviewEscalationDigest, error) {
	digests, err := h.ReviewEscalationDigests(ctx, req.After)
	if err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(req.Owner)
	level := strings.TrimSpace(req.EscalationLevel)
	out := make([]ReviewEscalationDigest, 0, len(digests))
	for _, digest := range digests {
		filtered := digest
		if owner != "" || level != "" {
			filtered.Groups = matchingEscalationGroups(digest.Groups, owner, level)
			total := 0
			for _, group := range filtered.Groups {
				total += group.Count
			}
			filtered.TotalEscalated = total
		}
		if len(filtered.Groups) == 0 {
			continue
		}
		out = append(out, filtered)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GeneratedAt.After(out[j].GeneratedAt)
	})
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

func matchingEscalationGroups(groups []ReviewEscalationGroup, owner, level string) []ReviewEscalationGroup {
	out := make([]ReviewEscalationGroup, 0, len(groups))
	for _, group := range groups {
		if owner != "" && group.Owner != owner {
			continue
		}
		if level != "" && group.EscalationLevel != level {
			continue
		}
		out = append(out, group)
	}
	return out
}

// ReviewHandoffWebhookPlan returns signed dry-run webhook deliveries for saved handoff snapshots.
func (h *NamespaceHandle) ReviewHandoffWebhookPlan(ctx context.Context, req ReviewHandoffWebhookRequest) ([]ReviewHandoffWebhookDelivery, error) {
	return h.reviewHandoffWebhookDeliveries(ctx, req, false)
}

// ReviewHandoffWebhookDeliver sends handoff webhook payloads when Execute is explicitly true.
func (h *NamespaceHandle) ReviewHandoffWebhookDeliver(ctx context.Context, req ReviewHandoffWebhookRequest) ([]ReviewHandoffWebhookDelivery, error) {
	if !req.Execute {
		return nil, fmt.Errorf("review handoff webhook: execute must be true")
	}
	return h.reviewHandoffWebhookDeliveries(ctx, req, true)
}

// ReviewHandoffWebhookRetry resends one unresolved failed handoff delivery candidate.
func (h *NamespaceHandle) ReviewHandoffWebhookRetry(ctx context.Context, req ReviewHandoffRetryRequest) (ReviewHandoffWebhookDelivery, error) {
	if !req.Execute {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: execute must be true")
	}
	if req.DigestEventID == uuid.Nil {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: digest event id is required")
	}
	target := strings.TrimSpace(req.TargetURL)
	if target == "" {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: target URL is required")
	}
	candidates, err := h.ReviewHandoffRetryCandidates(ctx, req.After)
	if err != nil {
		return ReviewHandoffWebhookDelivery{}, err
	}
	var candidate ReviewHandoffRetryCandidate
	found := false
	for _, item := range candidates {
		if item.DigestEventID == req.DigestEventID && item.TargetURL == target {
			candidate = item
			found = true
			break
		}
	}
	if !found {
		return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: candidate not found")
	}
	digests, err := h.ReviewEscalationDigests(ctx, req.After)
	if err != nil {
		return ReviewHandoffWebhookDelivery{}, err
	}
	for _, digest := range digests {
		if digest.EventID != req.DigestEventID {
			continue
		}
		filtered := digest
		if candidate.Owner != "" || candidate.EscalationLevel != "" {
			filtered.Groups = matchingEscalationGroups(digest.Groups, candidate.Owner, candidate.EscalationLevel)
			total := 0
			for _, group := range filtered.Groups {
				total += group.Count
			}
			filtered.TotalEscalated = total
		}
		if len(filtered.Groups) == 0 {
			return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: candidate groups not found")
		}
		deliveries, err := h.reviewHandoffWebhookDeliveriesForDigests(ctx, []ReviewEscalationDigest{filtered}, ReviewHandoffWebhookRequest{
			ReviewHandoffRequest: ReviewHandoffRequest{
				Owner:           candidate.Owner,
				EscalationLevel: candidate.EscalationLevel,
			},
			TargetURL:   target,
			Secret:      req.Secret,
			MaxAttempts: req.MaxAttempts,
			Now:         req.Now,
			Execute:     req.Execute,
			Timeout:     req.Timeout,
			HTTPClient:  req.HTTPClient,
		}, true)
		if err != nil {
			return ReviewHandoffWebhookDelivery{}, err
		}
		if len(deliveries) == 0 {
			return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: no delivery produced")
		}
		return deliveries[0], nil
	}
	return ReviewHandoffWebhookDelivery{}, fmt.Errorf("review handoff retry: digest not found")
}

func (h *NamespaceHandle) reviewHandoffWebhookDeliveries(ctx context.Context, req ReviewHandoffWebhookRequest, execute bool) ([]ReviewHandoffWebhookDelivery, error) {
	handoffs, err := h.ReviewHandoffs(ctx, req.ReviewHandoffRequest)
	if err != nil {
		return nil, err
	}
	return h.reviewHandoffWebhookDeliveriesForDigests(ctx, handoffs, req, execute)
}

func (h *NamespaceHandle) reviewHandoffWebhookDeliveriesForDigests(ctx context.Context, handoffs []ReviewEscalationDigest, req ReviewHandoffWebhookRequest, execute bool) ([]ReviewHandoffWebhookDelivery, error) {
	target := strings.TrimSpace(req.TargetURL)
	if target == "" {
		return nil, fmt.Errorf("review handoff webhook: target URL is required")
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("review handoff webhook: invalid target URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("review handoff webhook: target URL must be http or https")
	}
	plannedAt := req.Now
	if plannedAt.IsZero() {
		plannedAt = time.Now().UTC()
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	mode := "dry-run"
	if execute {
		mode = "execute"
	}
	out := make([]ReviewHandoffWebhookDelivery, 0, len(handoffs))
	for _, digest := range handoffs {
		payload, err := json.Marshal(digest)
		if err != nil {
			return nil, fmt.Errorf("review handoff webhook: marshal payload: %w", err)
		}
		sum := sha256.Sum256(payload)
		payloadSHA := hex.EncodeToString(sum[:])
		headers := map[string]string{
			"Content-Type":                 "application/json",
			"X-ContextDB-Delivery-Mode":    mode,
			"X-ContextDB-Handoff-Event-ID": digest.EventID.String(),
			"X-ContextDB-Payload-SHA256":   payloadSHA,
		}
		signature := ""
		if req.Secret != "" {
			mac := hmac.New(sha256.New, []byte(req.Secret))
			_, _ = mac.Write(payload)
			signature = "sha256=" + hex.EncodeToString(mac.Sum(nil))
			headers["X-ContextDB-Signature"] = signature
		}
		delivery := ReviewHandoffWebhookDelivery{
			TargetURL:       target,
			Method:          "POST",
			DryRun:          !execute,
			EventID:         digest.EventID,
			Namespace:       digest.Namespace,
			GeneratedAt:     digest.GeneratedAt,
			PlannedAt:       plannedAt,
			Owner:           strings.TrimSpace(req.Owner),
			EscalationLevel: strings.TrimSpace(req.EscalationLevel),
			TotalEscalated:  digest.TotalEscalated,
			Groups:          digest.Groups,
			Payload:         json.RawMessage(payload),
			PayloadSHA256:   payloadSHA,
			Signature:       signature,
			Headers:         headers,
			Attempt:         1,
			MaxAttempts:     maxAttempts,
			NextRetryAfter:  time.Minute,
		}
		if execute {
			delivery = executeReviewHandoffWebhook(ctx, req, delivery)
			if err := h.recordReviewHandoffDeliveryReceipt(ctx, delivery); err != nil {
				return nil, err
			}
		}
		out = append(out, delivery)
	}
	return out, nil
}

func executeReviewHandoffWebhook(ctx context.Context, req ReviewHandoffWebhookRequest, delivery ReviewHandoffWebhookDelivery) ReviewHandoffWebhookDelivery {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := req.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, delivery.Method, delivery.TargetURL, bytes.NewReader(delivery.Payload))
	if err != nil {
		delivery.Executed = true
		delivery.Error = err.Error()
		return delivery
	}
	for key, value := range delivery.Headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := client.Do(httpReq)
	delivery.Executed = true
	if err != nil {
		delivery.Error = err.Error()
		return delivery
	}
	defer resp.Body.Close()
	delivery.StatusCode = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		delivery.Error = err.Error()
		return delivery
	}
	delivery.ResponseBody = string(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		delivery.Error = fmt.Sprintf("webhook returned status %d", resp.StatusCode)
	}
	return delivery
}

func (h *NamespaceHandle) recordReviewHandoffDeliveryReceipt(ctx context.Context, delivery ReviewHandoffWebhookDelivery) error {
	responseSHA := ""
	if delivery.ResponseBody != "" {
		sum := sha256.Sum256([]byte(delivery.ResponseBody))
		responseSHA = hex.EncodeToString(sum[:])
	}
	receipt := ReviewHandoffDeliveryReceipt{
		ReceiptID:       uuid.New(),
		DigestEventID:   delivery.EventID,
		Namespace:       h.cfg.ID,
		TargetURL:       delivery.TargetURL,
		DeliveredAt:     time.Now().UTC(),
		Owner:           delivery.Owner,
		EscalationLevel: delivery.EscalationLevel,
		Success:         delivery.Executed && delivery.Error == "" && delivery.StatusCode >= 200 && delivery.StatusCode < 300,
		StatusCode:      delivery.StatusCode,
		PayloadSHA256:   delivery.PayloadSHA256,
		ResponseSHA256:  responseSHA,
		Error:           delivery.Error,
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("review handoff receipt: marshal: %w", err)
	}
	event := store.Event{
		ID:        receipt.ReceiptID,
		Namespace: h.cfg.ID,
		Type:      store.EventReviewHandoffReceipt,
		Payload:   payload,
		TxTime:    receipt.DeliveredAt,
	}
	if err := h.db.log.Append(ctx, event); err != nil {
		return fmt.Errorf("review handoff receipt: append event: %w", err)
	}
	return nil
}

// ReviewHandoffDeliveryReceipts returns delivery receipt audit records after the given time.
func (h *NamespaceHandle) ReviewHandoffDeliveryReceipts(ctx context.Context, after time.Time) ([]ReviewHandoffDeliveryReceipt, error) {
	events, err := h.db.log.SinceAll(ctx, h.cfg.ID, after)
	if err != nil {
		return nil, fmt.Errorf("review handoff receipts: %w", err)
	}
	out := make([]ReviewHandoffDeliveryReceipt, 0, len(events))
	for _, event := range events {
		if event.Type != store.EventReviewHandoffReceipt {
			continue
		}
		var receipt ReviewHandoffDeliveryReceipt
		if err := json.Unmarshal(event.Payload, &receipt); err != nil {
			return nil, fmt.Errorf("review handoff receipts: decode %s: %w", event.ID, err)
		}
		receipt.ReceiptID = event.ID
		if receipt.Namespace == "" {
			receipt.Namespace = event.Namespace
		}
		if receipt.DeliveredAt.IsZero() {
			receipt.DeliveredAt = event.TxTime
		}
		out = append(out, receipt)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].DeliveredAt.After(out[j].DeliveredAt)
	})
	return out, nil
}

// ReviewHandoffRetryCandidates returns latest failed handoff delivery groups without sending retries.
func (h *NamespaceHandle) ReviewHandoffRetryCandidates(ctx context.Context, after time.Time) ([]ReviewHandoffRetryCandidate, error) {
	receipts, err := h.ReviewHandoffDeliveryReceipts(ctx, after)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(receipts, func(i, j int) bool {
		return receipts[i].DeliveredAt.Before(receipts[j].DeliveredAt)
	})
	candidates := map[string]ReviewHandoffRetryCandidate{}
	for _, receipt := range receipts {
		key := receipt.DigestEventID.String() + "\x00" + receipt.TargetURL
		candidate := candidates[key]
		candidate.Attempts++
		if receipt.Success {
			delete(candidates, key)
			continue
		}
		candidate.DigestEventID = receipt.DigestEventID
		candidate.TargetURL = receipt.TargetURL
		candidate.LastReceiptID = receipt.ReceiptID
		candidate.LastAttemptAt = receipt.DeliveredAt
		candidate.Owner = receipt.Owner
		candidate.EscalationLevel = receipt.EscalationLevel
		candidate.LastStatusCode = receipt.StatusCode
		candidate.PayloadSHA256 = receipt.PayloadSHA256
		candidate.LastError = receipt.Error
		candidates[key] = candidate
	}
	out := make([]ReviewHandoffRetryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastAttemptAt.After(out[j].LastAttemptAt)
	})
	return out, nil
}

// ReviewHandoffRetryRecommendations returns read-only backoff guidance for unresolved failed handoff deliveries.
func (h *NamespaceHandle) ReviewHandoffRetryRecommendations(ctx context.Context, after time.Time, now time.Time) ([]ReviewHandoffRetryRecommendation, error) {
	candidates, err := h.ReviewHandoffRetryCandidates(ctx, after)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]ReviewHandoffRetryRecommendation, 0, len(candidates))
	for _, candidate := range candidates {
		delay := reviewHandoffRetryBackoff(candidate.Attempts)
		recommendedAfter := candidate.LastAttemptAt.Add(delay)
		ready := !now.Before(recommendedAfter)
		reason := "waiting_for_backoff"
		if ready {
			reason = "ready_for_operator_retry"
		}
		out = append(out, ReviewHandoffRetryRecommendation{
			ReviewHandoffRetryCandidate: candidate,
			RecommendedAfter:            recommendedAfter,
			DelaySeconds:                int(delay.Seconds()),
			Ready:                       ready,
			Reason:                      reason,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ready != out[j].Ready {
			return out[i].Ready
		}
		return out[i].RecommendedAfter.Before(out[j].RecommendedAfter)
	})
	return out, nil
}

// ReviewHandoffRetryFatigue groups retry recommendations by endpoint without sending retries.
func (h *NamespaceHandle) ReviewHandoffRetryFatigue(ctx context.Context, after time.Time, now time.Time) ([]ReviewHandoffRetryFatigueSummary, error) {
	recommendations, err := h.ReviewHandoffRetryRecommendations(ctx, after, now)
	if err != nil {
		return nil, err
	}
	summaries := map[string]*ReviewHandoffRetryFatigueSummary{}
	statusFamilies := map[string]map[string]int{}
	for _, recommendation := range recommendations {
		summary := summaries[recommendation.TargetURL]
		if summary == nil {
			summary = &ReviewHandoffRetryFatigueSummary{TargetURL: recommendation.TargetURL}
			summaries[recommendation.TargetURL] = summary
			statusFamilies[recommendation.TargetURL] = map[string]int{}
		}
		summary.Candidates++
		summary.TotalAttempts += recommendation.Attempts
		if recommendation.Ready {
			summary.Ready++
		} else {
			summary.Waiting++
		}
		family := reviewHandoffStatusFamily(recommendation.LastStatusCode)
		statusFamilies[recommendation.TargetURL][family]++
		if recommendation.LastAttemptAt.After(summary.LastAttemptAt) {
			summary.LastAttemptAt = recommendation.LastAttemptAt
			summary.LastStatusCode = recommendation.LastStatusCode
			summary.LastError = recommendation.LastError
		}
	}
	out := make([]ReviewHandoffRetryFatigueSummary, 0, len(summaries))
	for targetURL, summary := range summaries {
		for family, count := range statusFamilies[targetURL] {
			summary.StatusFamilies = append(summary.StatusFamilies, ReviewHandoffRetryStatusFamilyCount{
				Family: family,
				Count:  count,
			})
		}
		sort.SliceStable(summary.StatusFamilies, func(i, j int) bool {
			return summary.StatusFamilies[i].Family < summary.StatusFamilies[j].Family
		})
		out = append(out, *summary)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ready != out[j].Ready {
			return out[i].Ready > out[j].Ready
		}
		if out[i].TotalAttempts != out[j].TotalAttempts {
			return out[i].TotalAttempts > out[j].TotalAttempts
		}
		return out[i].LastAttemptAt.After(out[j].LastAttemptAt)
	})
	return out, nil
}

func reviewHandoffRetryBackoff(attempts int) time.Duration {
	if attempts <= 1 {
		return time.Minute
	}
	delay := time.Minute
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= time.Hour {
			return time.Hour
		}
	}
	return delay
}

func reviewHandoffStatusFamily(statusCode int) string {
	if statusCode <= 0 {
		return "network_error"
	}
	return fmt.Sprintf("%dxx", statusCode/100)
}

func formatRetryStatusFamilies(counts []ReviewHandoffRetryStatusFamilyCount) string {
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", count.Family, count.Count))
	}
	return strings.Join(parts, ", ")
}

func formatRetryLatestFailure(summary ReviewHandoffRetryFatigueSummary) string {
	parts := []string{}
	if summary.LastStatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status %d", summary.LastStatusCode))
	}
	if strings.TrimSpace(summary.LastError) != "" {
		parts = append(parts, summary.LastError)
	}
	if !summary.LastAttemptAt.IsZero() {
		parts = append(parts, summary.LastAttemptAt.UTC().Format(time.RFC3339))
	}
	return strings.Join(parts, "; ")
}

func markdownInline(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "'")
}

func markdownTable(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "`", "'")
}

// Explain returns a narrative report explaining what is known about a node.
func (h *NamespaceHandle) Explain(ctx context.Context, nodeID uuid.UUID) (*retrieval.NarrativeReport, error) {
	formatter := retrieval.NewNarrativeFormatter(h.db.graph, h.db.vecs)
	return formatter.Explain(ctx, h.cfg.ID, nodeID)
}

// KnowledgeGaps detects sparse semantic regions in this namespace.
func (h *NamespaceHandle) KnowledgeGaps(ctx context.Context, req GapRequest) (*retrieval.GapReport, error) {
	detector := retrieval.NewGapDetector(h.db.graph, h.db.vecs)
	gaps, err := detector.DetectGaps(ctx, h.cfg.ID, retrieval.GapQuery{
		TopK:       req.TopK,
		MinGapSize: req.MinGapSize,
		MaxGaps:    req.MaxGaps,
	})
	if err != nil {
		return nil, err
	}
	nodes, err := h.db.graph.ValidAt(ctx, h.cfg.ID, time.Now(), nil)
	if err != nil {
		return nil, err
	}
	return retrieval.BuildGapReport(h.cfg.ID, gaps, len(nodes)), nil
}

// AcquisitionPlan turns detected knowledge gaps and weak claims into acquisition tasks.
func (h *NamespaceHandle) AcquisitionPlan(ctx context.Context, req AcquisitionPlanRequest) (*AcquisitionPlan, error) {
	budget := req.Budget
	if budget <= 0 {
		budget = 10
	}
	gapReport, err := h.KnowledgeGaps(ctx, GapRequest{
		TopK:       req.TopK,
		MinGapSize: req.MinGapSize,
		MaxGaps:    req.MaxGaps,
	})
	if err != nil {
		return nil, err
	}
	plan := &AcquisitionPlan{
		Namespace:     h.cfg.ID,
		CoverageScore: gapReport.CoverageScore,
		TotalNodes:    gapReport.TotalNodes,
		Tasks:         make([]AcquisitionTask, 0, budget),
	}
	for _, gap := range gapReport.Gaps {
		plan.Tasks = append(plan.Tasks, AcquisitionTask{
			ID:            "gap:" + gap.ID.String(),
			Type:          "research_gap",
			Priority:      clampPriority(1.0 - gap.DensityScore + (1.0-gap.ConfidenceGap)*0.25),
			Description:   "Sparse knowledge region near: " + strings.Join(gap.NearestTopics, "; "),
			Prompt:        acquisitionPrompt("research_gap", gap.NearestTopics),
			NearestTopics: gap.NearestTopics,
		})
	}

	learner := retrieval.NewActiveLearner(h.db.graph)
	suggestions, err := learner.Suggest(ctx, h.cfg.ID, budget)
	if err != nil {
		return nil, err
	}
	for _, suggestion := range suggestions {
		plan.Tasks = append(plan.Tasks, AcquisitionTask{
			ID:             fmt.Sprintf("%s:%s", suggestion.Type, reviewIDsKey(suggestion.RelatedNodeIDs)),
			Type:           string(suggestion.Type),
			Priority:       clampPriority(suggestion.Priority),
			Description:    suggestion.Description,
			Prompt:         acquisitionPrompt(string(suggestion.Type), nil),
			RelatedNodeIDs: suggestion.RelatedNodeIDs,
		})
	}
	sort.SliceStable(plan.Tasks, func(i, j int) bool {
		return plan.Tasks[i].Priority > plan.Tasks[j].Priority
	})
	if len(plan.Tasks) > budget {
		plan.Tasks = plan.Tasks[:budget]
	}
	return plan, nil
}

func (h *NamespaceHandle) rankEvidence(ctx context.Context, nodeID uuid.UUID, maxDepth int) (RankEvidence, error) {
	chain, err := retrieval.TraceInferenceChain(ctx, h.db.graph, h.cfg.ID, nodeID, maxDepth)
	if err != nil {
		return RankEvidence{}, fmt.Errorf("explain rank: trace evidence: %w", err)
	}
	if chain == nil {
		return RankEvidence{}, nil
	}
	evidence := RankEvidence{
		CompoundConfidence: chain.CompoundConfidence,
		SupportCount:       len(chain.Links),
		Links:              make([]RankEvidenceLink, 0, len(chain.Links)),
	}
	for _, link := range chain.Links {
		evidence.Links = append(evidence.Links, RankEvidenceLink{
			NodeID:     link.Node.ID,
			EdgeID:     link.Edge.ID,
			EdgeWeight: link.Edge.Weight,
			Confidence: link.Confidence,
			Text:       core.NodeText(link.Node),
		})
	}
	return evidence, nil
}

// ── Enhanced SDK (Phase 6) ────────────────────────────────────────────────

// WriteBatch writes multiple items in a single call. Returns results
// in the same order as requests. Partial failures are possible — if one
// write fails the remaining writes still execute. The first error
// encountered (if any) is returned alongside the partial results.
func (h *NamespaceHandle) WriteBatch(ctx context.Context, reqs []WriteRequest) ([]WriteResult, error) {
	results := make([]WriteResult, len(reqs))
	var firstErr error
	for i, req := range reqs {
		res, err := h.Write(ctx, req)
		if err != nil {
			results[i] = WriteResult{Reason: err.Error()}
			if firstErr == nil {
				firstErr = fmt.Errorf("WriteBatch[%d]: %w", i, err)
			}
			continue
		}
		results[i] = res
	}
	return results, firstErr
}

// WriteBatchOrdered writes nodes in dependency order. Requests are
// topologically sorted by DependsOn before writing. If there is a
// cycle, an error is returned. Requests without dependencies are
// written first. Results are indexed to match the original request
// slice, not the execution order.
func (h *NamespaceHandle) WriteBatchOrdered(ctx context.Context, reqs []WriteRequest) ([]WriteResult, error) {
	if len(reqs) <= 1 {
		return h.WriteBatch(ctx, reqs)
	}

	ordered, err := topoSort(reqs)
	if err != nil {
		return nil, err
	}

	results := make([]WriteResult, len(reqs))
	for _, idx := range ordered {
		res, err := h.Write(ctx, reqs[idx])
		results[idx] = res
		if err != nil {
			return results, fmt.Errorf("write %d: %w", idx, err)
		}
	}
	return results, nil
}

// GetNode retrieves a single node by ID from this namespace.
func (h *NamespaceHandle) GetNode(ctx context.Context, id uuid.UUID) (*core.Node, error) {
	return h.db.graph.GetNode(ctx, h.cfg.ID, id)
}

// WalkResult holds a node discovered during graph traversal together
// with the depth at which it was found and the full path (as node IDs)
// from the seed to this node.
type WalkResult struct {
	Node  core.Node
	Depth int
	Path  []uuid.UUID // node IDs from seed to this node
}

// Walk performs a breadth-first graph traversal from the given seed nodes.
// It uses EdgesFrom to expand outward level-by-level so that per-node
// depth and path information can be tracked (the lower-level store.Walk
// method returns flat results without this metadata).
func (h *NamespaceHandle) Walk(ctx context.Context, seedIDs []uuid.UUID, maxDepth int) ([]WalkResult, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	type entry struct {
		id   uuid.UUID
		path []uuid.UUID
	}

	visited := make(map[uuid.UUID]bool, len(seedIDs))
	var results []WalkResult

	// Initialise the frontier with the seed nodes.
	queue := make([]entry, 0, len(seedIDs))
	for _, sid := range seedIDs {
		if visited[sid] {
			continue
		}
		visited[sid] = true
		queue = append(queue, entry{id: sid, path: []uuid.UUID{sid}})

		node, err := h.db.graph.GetNode(ctx, h.cfg.ID, sid)
		if err != nil {
			return nil, fmt.Errorf("Walk: get seed %s: %w", sid, err)
		}
		if node != nil {
			results = append(results, WalkResult{
				Node:  *node,
				Depth: 0,
				Path:  []uuid.UUID{sid},
			})
		}
	}

	for depth := 1; depth <= maxDepth; depth++ {
		var nextQueue []entry
		for _, cur := range queue {
			edges, err := h.db.graph.EdgesFrom(ctx, h.cfg.ID, cur.id, nil)
			if err != nil {
				return nil, fmt.Errorf("Walk: edges from %s at depth %d: %w", cur.id, depth, err)
			}
			for _, e := range edges {
				if visited[e.Dst] {
					continue
				}
				visited[e.Dst] = true

				newPath := make([]uuid.UUID, len(cur.path)+1)
				copy(newPath, cur.path)
				newPath[len(cur.path)] = e.Dst

				node, err := h.db.graph.GetNode(ctx, h.cfg.ID, e.Dst)
				if err != nil {
					return nil, fmt.Errorf("Walk: get node %s at depth %d: %w", e.Dst, depth, err)
				}
				if node != nil {
					results = append(results, WalkResult{
						Node:  *node,
						Depth: depth,
						Path:  newPath,
					})
				}
				nextQueue = append(nextQueue, entry{id: e.Dst, path: newPath})
			}
		}
		queue = nextQueue
	}

	return results, nil
}

// AddEdge creates an edge between two nodes in this namespace.
// If edge.Namespace is empty it is set to the namespace of this handle.
// If edge.ID is zero a new UUID is assigned.
func (h *NamespaceHandle) AddEdge(ctx context.Context, edge core.Edge) error {
	if edge.Namespace == "" {
		edge.Namespace = h.cfg.ID
	}
	if edge.ID == uuid.Nil {
		edge.ID = uuid.New()
	}
	if edge.TxTime.IsZero() {
		edge.TxTime = time.Now()
	}
	if edge.ValidFrom.IsZero() {
		edge.ValidFrom = time.Now()
	}
	return h.db.graph.UpsertEdge(ctx, edge)
}

// History returns all versions of a node, ordered oldest-first by
// transaction time.
func (h *NamespaceHandle) History(ctx context.Context, nodeID uuid.UUID) ([]core.Node, error) {
	return h.db.graph.History(ctx, h.cfg.ID, nodeID)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type feedbackUpdate struct {
	action          string
	confidenceDelta float64
	confidenceSet   *float64
	utilityDelta    float64
	sourceValidated *bool
	quality         int
	reason          string
}

func (h *NamespaceHandle) applyFeedback(ctx context.Context, nodeID uuid.UUID, update feedbackUpdate) (FeedbackResult, error) {
	node, err := h.db.graph.GetNode(ctx, h.cfg.ID, nodeID)
	if err != nil {
		return FeedbackResult{}, fmt.Errorf("%s: get node: %w", update.action, err)
	}
	if node == nil {
		return FeedbackResult{}, fmt.Errorf("%s: node %s not found", update.action, nodeID)
	}
	if node.Properties == nil {
		node.Properties = make(map[string]any)
	}

	now := time.Now()
	baseConf := node.Confidence
	if baseConf == 0 {
		baseConf = 0.5
	}
	if update.confidenceSet != nil {
		node.Confidence = clampFeedback(*update.confidenceSet)
	} else if update.confidenceDelta != 0 {
		node.Confidence = clampFeedback(baseConf + update.confidenceDelta)
	}

	utility := propertyFloat(node.Properties, "utility", 1.0)
	if update.utilityDelta != 0 {
		utility = clampFeedback(utility + update.utilityDelta)
		node.Properties["utility"] = utility
	}
	if update.quality != 0 {
		sm2 := core.Sm2FromProperties(node.Properties)
		node.Properties = sm2.Update(update.quality).ToProperties(node.Properties)
	}

	countKey := update.action + "_count"
	node.Properties[countKey] = propertyFloat(node.Properties, countKey, 0) + 1
	node.Properties[update.action+"_at"] = now.Format(time.RFC3339)
	if update.reason != "" {
		node.Properties[update.action+"_reason"] = update.reason
	}
	node.TxTime = now

	result := FeedbackResult{
		NodeID:     nodeID,
		Action:     update.action,
		Confidence: node.Confidence,
		Utility:    utility,
		Reason:     update.reason,
	}

	sourceID, _ := node.Properties["source_id"].(string)
	result.SourceID = sourceID
	if sourceID != "" && update.sourceValidated != nil {
		src, err := h.db.graph.GetSourceByExternalID(ctx, h.cfg.ID, sourceID)
		if err != nil {
			return FeedbackResult{}, fmt.Errorf("%s: get source: %w", update.action, err)
		}
		if src != nil {
			src.BayesianUpdate(*update.sourceValidated)
			if err := h.db.graph.UpsertSource(ctx, *src); err != nil {
				return FeedbackResult{}, fmt.Errorf("%s: update source: %w", update.action, err)
			}
			result.SourceCredibility = src.EffectiveCredibility()
		}
	}

	if err := h.db.graph.UpsertNode(ctx, *node); err != nil {
		return FeedbackResult{}, fmt.Errorf("%s: upsert node: %w", update.action, err)
	}
	if latest, err := h.db.graph.GetNode(ctx, h.cfg.ID, nodeID); err == nil && latest != nil {
		node = latest
	}
	if err := h.appendFeedbackEvent(ctx, node, result, update, now); err != nil {
		return FeedbackResult{}, err
	}
	return result, nil
}

func (h *NamespaceHandle) appendFeedbackEvent(ctx context.Context, node *core.Node, result FeedbackResult, update feedbackUpdate, txTime time.Time) error {
	feedback := FeedbackEvent{
		Namespace:         h.cfg.ID,
		NodeID:            result.NodeID,
		NodeVersion:       node.Version,
		Action:            result.Action,
		Confidence:        result.Confidence,
		Utility:           result.Utility,
		SourceID:          result.SourceID,
		SourceCredibility: result.SourceCredibility,
		Reason:            result.Reason,
		Quality:           update.quality,
		TxTime:            txTime,
	}
	payload, err := json.Marshal(feedback)
	if err != nil {
		return fmt.Errorf("%s: marshal feedback event: %w", update.action, err)
	}
	if err := h.db.log.Append(ctx, store.Event{
		Namespace: h.cfg.ID,
		Type:      store.EventFeedback,
		Payload:   payload,
		TxTime:    txTime,
	}); err != nil {
		return fmt.Errorf("%s: append feedback event: %w", update.action, err)
	}
	return nil
}

func reviewFeedbackPriority(event FeedbackEvent) float64 {
	switch event.Action {
	case "refuted":
		return 1.0
	case "stale":
		return 0.75
	default:
		return 0.5
	}
}

func reviewSuggestion(action string) string {
	switch action {
	case "refuted":
		return "verify the refutation and retract or replace the claim"
	case "stale":
		return "refresh the claim or mark its replacement"
	default:
		return "review the claim"
	}
}

func nodeSourceID(node core.Node) string {
	sourceID, _ := node.Properties["source_id"].(string)
	return sourceID
}

func reviewIDsKey(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func sourceTrustAnomalyItems(events []FeedbackEvent, req ReviewQueueRequest) []ReviewItem {
	dropThreshold := req.SourceTrustDropThreshold
	lowTrustThreshold := req.SourceTrustThreshold
	refutationThreshold := req.SourceRefutationThreshold

	type sourceState struct {
		firstCredibility  float64
		latestCredibility float64
		latestAt          time.Time
		refutations       int
		nodeIDs           []uuid.UUID
	}
	states := map[string]*sourceState{}
	for _, event := range events {
		if event.SourceID == "" || event.SourceCredibility == 0 {
			continue
		}
		state := states[event.SourceID]
		if state == nil {
			state = &sourceState{firstCredibility: event.SourceCredibility}
			states[event.SourceID] = state
		}
		state.latestCredibility = event.SourceCredibility
		state.latestAt = event.TxTime
		if event.NodeID != uuid.Nil {
			state.nodeIDs = append(state.nodeIDs, event.NodeID)
		}
		if event.Action == "refuted" {
			state.refutations++
		}
	}

	items := make([]ReviewItem, 0, len(states))
	for sourceID, state := range states {
		drop := state.firstCredibility - state.latestCredibility
		reasons := []string{}
		priority := 0.0
		action := ""
		if dropThreshold > 0 && drop >= dropThreshold {
			reasons = append(reasons, fmt.Sprintf("source credibility dropped %.2f", drop))
			priority = maxFloat(priority, 0.65+drop)
			action = "credibility_drop"
		}
		if lowTrustThreshold > 0 && state.latestCredibility <= lowTrustThreshold {
			reasons = append(reasons, fmt.Sprintf("source credibility %.2f is at or below %.2f", state.latestCredibility, lowTrustThreshold))
			priority = maxFloat(priority, 0.75+(lowTrustThreshold-state.latestCredibility))
			if action == "" {
				action = "low_trust"
			}
		}
		if refutationThreshold > 0 && state.refutations >= refutationThreshold {
			reasons = append(reasons, fmt.Sprintf("source has %d recent refutations", state.refutations))
			priority = maxFloat(priority, 0.8+float64(state.refutations)*0.05)
			if action == "" {
				action = "repeated_refutations"
			}
		}
		if len(reasons) == 0 {
			continue
		}
		items = append(items, ReviewItem{
			ID:         "source_trust:" + sourceID,
			Type:       "source_trust_anomaly",
			Priority:   priority,
			Reason:     strings.Join(reasons, "; "),
			NodeIDs:    uniqueUUIDs(state.nodeIDs),
			SourceID:   sourceID,
			Action:     action,
			CreatedAt:  state.latestAt,
			Suggested:  "review recent claims from this source and decide whether to relabel, exclude, or verify manually",
			Confidence: state.latestCredibility,
		})
	}
	return items
}

func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func latestReviewDecisions(decisions []ReviewDecision) map[string]ReviewDecision {
	latest := make(map[string]ReviewDecision, len(decisions))
	for _, decision := range decisions {
		if existing, ok := latest[decision.ReviewID]; !ok || decision.TxTime.After(existing.TxTime) {
			latest[decision.ReviewID] = decision
		}
	}
	return latest
}

func applyReviewDecisions(items []ReviewItem, decisions map[string]ReviewDecision, now time.Time) []ReviewItem {
	if len(decisions) == 0 {
		return items
	}
	out := make([]ReviewItem, 0, len(items))
	for _, item := range items {
		decision, ok := decisions[item.ID]
		if !ok {
			out = append(out, item)
			continue
		}
		if decision.Status == "resolved" {
			continue
		}
		if decision.Status == "snoozed" && decision.RecheckAt.After(now) {
			continue
		}
		item.Status = decision.Status
		item.Owner = decision.Owner
		item.Decision = decision.Decision
		item.Note = decision.Note
		item.RecheckAt = decision.RecheckAt
		item.ReviewedAt = decision.TxTime
		out = append(out, item)
	}
	return out
}

func applyReviewEscalations(items []ReviewItem, req ReviewQueueRequest, now time.Time) []ReviewItem {
	if req.EscalationAfter <= 0 {
		return items
	}
	sourcePriority := req.SourceAnomalyEscalationPriority
	if sourcePriority == 0 {
		sourcePriority = 0.9
	}
	for i := range items {
		item := &items[i]
		status := reviewItemStatus(*item)
		switch {
		case (status == "assigned" || status == "snoozed") && !item.ReviewedAt.IsZero():
			age := now.Sub(item.ReviewedAt)
			if age >= req.EscalationAfter {
				item.Escalated = true
				item.EscalationLevel = "review_overdue"
				item.EscalationReason = fmt.Sprintf("%s review has waited %.1f hours", status, age.Hours())
				item.EscalationAgeHours = age.Hours()
				item.Priority = maxFloat(item.Priority, 1.0+age.Hours()/168.0)
			}
		case item.Type == "source_trust_anomaly" && item.Priority >= sourcePriority && !item.CreatedAt.IsZero():
			age := now.Sub(item.CreatedAt)
			if age >= req.EscalationAfter {
				item.Escalated = true
				item.EscalationLevel = "source_anomaly_high"
				item.EscalationReason = fmt.Sprintf("source anomaly priority %.2f has waited %.1f hours", item.Priority, age.Hours())
				item.EscalationAgeHours = age.Hours()
				item.Priority = maxFloat(item.Priority, 1.1+age.Hours()/168.0)
			}
		}
	}
	return items
}

func filterReviewItems(items []ReviewItem, req ReviewQueueRequest) []ReviewItem {
	types := stringSet(req.Types)
	sourceID := strings.TrimSpace(req.SourceID)
	status := strings.TrimSpace(req.Status)
	owner := strings.TrimSpace(req.Owner)
	if len(types) == 0 && sourceID == "" && status == "" && owner == "" {
		return items
	}
	out := make([]ReviewItem, 0, len(items))
	for _, item := range items {
		if len(types) > 0 && !types[item.Type] {
			continue
		}
		if sourceID != "" && item.SourceID != sourceID {
			continue
		}
		if status != "" && reviewItemStatus(item) != status {
			continue
		}
		if owner != "" && item.Owner != owner {
			continue
		}
		out = append(out, item)
	}
	return out
}

func reviewItemStatus(item ReviewItem) string {
	if strings.TrimSpace(item.Status) == "" {
		return "open"
	}
	return item.Status
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = true
	}
	return out
}

func scoreRankNode(node core.Node, vector []float32, params core.ScoreParams) core.ScoredNode {
	similarity := 0.0
	source := "score"
	if len(vector) > 0 && len(node.Vector) > 0 {
		similarity = core.CosineSimilarity(vector, node.Vector) * 0.45
		source = "vector"
	}
	scored := core.ScoreNode(node, similarity, propertyFloat(node.Properties, "utility", 1.0), params)
	scored.RetrievalSource = source
	return scored
}

func newRankedNodeExplanation(scored core.ScoredNode) RankedNodeExplanation {
	return RankedNodeExplanation{
		NodeID:          scored.Node.ID,
		Text:            core.NodeText(scored.Node),
		Score:           scored.Score,
		SimilarityScore: scored.SimilarityScore,
		ConfidenceScore: scored.ConfidenceScore,
		RecencyScore:    scored.RecencyScore,
		UtilityScore:    scored.UtilityScore,
		ScoreBreakdown:  scored.Breakdown,
		RetrievalSource: scored.RetrievalSource,
	}
}

func rankFactorDeltas(left, right core.ScoreBreakdown) []RankFactorDelta {
	factors := []RankFactorDelta{
		{Factor: "similarity", NodeContribution: left.Similarity, OtherContribution: right.Similarity},
		{Factor: "confidence", NodeContribution: left.Confidence, OtherContribution: right.Confidence},
		{Factor: "recency", NodeContribution: left.Recency, OtherContribution: right.Recency},
		{Factor: "utility", NodeContribution: left.Utility, OtherContribution: right.Utility},
	}
	for i := range factors {
		factors[i].Delta = factors[i].NodeContribution - factors[i].OtherContribution
	}
	sort.SliceStable(factors, func(i, j int) bool {
		return absFloat(factors[i].Delta) > absFloat(factors[j].Delta)
	})
	return factors
}

func rankSummary(winner, loser core.ScoredNode) string {
	factors := rankFactorDeltas(winner.Breakdown, loser.Breakdown)
	if len(factors) == 0 || absFloat(factors[0].Delta) == 0 {
		return fmt.Sprintf("%s ranks above %s by %.4f points.", winner.Node.ID, loser.Node.ID, winner.Score-loser.Score)
	}
	return fmt.Sprintf("%s ranks above %s by %.4f points; %s contributes the largest difference.",
		winner.Node.ID, loser.Node.ID, winner.Score-loser.Score, factors[0].Factor)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func clampPriority(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func acquisitionPrompt(taskType string, topics []string) string {
	switch taskType {
	case "research_gap":
		if len(topics) == 0 {
			return "Find high-credibility sources that fill this sparse knowledge region, then ingest concise claims with source IDs."
		}
		return "Research the gap around: " + strings.Join(topics, "; ") + ". Prefer primary or high-credibility sources and ingest concise supporting claims."
	case "verify_claim", "low_confidence":
		return "Find independent evidence that validates or refutes the related claim, then apply feedback or ingest counter-evidence."
	case "refresh_stale":
		return "Find current information for the related stale claim, then update or supersede it with fresh source-backed evidence."
	case "high_utility":
		return "Find stronger sources for this frequently used claim and ingest corroborating evidence."
	default:
		return "Acquire source-backed evidence and ingest it into this namespace."
	}
}

func propertyFloat(props map[string]any, key string, fallback float64) float64 {
	switch v := props[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return fallback
	}
}

func clampFeedback(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func ptrBool(v bool) *bool {
	return &v
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && len(substr) > 0 &&
			func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}())
}
