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
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/ingest"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
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
	// Phase 5 — not yet implemented; falls back to embedded.
	ModeRemote Mode = "remote"
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
		db.graph = memstore.NewGraphStore()
		db.vecs = memstore.NewVectorIndex()
		db.kv = memstore.NewKVStore()
		db.log = memstore.NewEventLog()
		db.logger.Info("contextdb connected", "mode", "embedded")
		return nil

	case ModeStandard:
		// Phase 2: Postgres + pgvector
		// For now falls back to embedded with a warning.
		db.logger.Warn("ModeStandard not yet implemented, falling back to embedded")
		db.graph = memstore.NewGraphStore()
		db.vecs = memstore.NewVectorIndex()
		db.kv = memstore.NewKVStore()
		db.log = memstore.NewEventLog()
		return nil

	case ModeRemote:
		// Phase 5: HTTP client to remote server
		db.logger.Warn("ModeRemote not yet implemented, falling back to embedded")
		db.graph = memstore.NewGraphStore()
		db.vecs = memstore.NewVectorIndex()
		db.kv = memstore.NewKVStore()
		db.log = memstore.NewEventLog()
		return nil

	default:
		return fmt.Errorf("unknown mode: %q", db.opts.Mode)
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
	db.logger.Info("contextdb closed")
	return nil
}

// Stats returns connection pool and metric statistics.
// Analogous to sql.DB.Stats.
func (db *DB) Stats() DBStats {
	snap := db.metrics.RetrievalLatency.Snapshot()
	return DBStats{
		Mode:             db.opts.Mode,
		RetrievalTotal:   db.metrics.RetrievalTotal.Value(),
		RetrievalErrors:  db.metrics.RetrievalErrors.Value(),
		IngestTotal:      db.metrics.IngestTotal.Value(),
		IngestAdmitted:   db.metrics.IngestAdmitted.Value(),
		IngestRejected:   db.metrics.IngestRejected.Value(),
		LatencyP50Us:     snap.P(50),
		LatencyP95Us:     snap.P(95),
		LatencyMeanUs:    snap.Mean(),
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

// Write ingests a new claim, memory, or fact into the namespace.
// It runs through the admission gate (credibility floor, near-duplicate
// check, novelty threshold) before writing.
func (h *NamespaceHandle) Write(ctx context.Context, req WriteRequest) (WriteResult, error) {
	start := time.Now()
	h.db.metrics.IngestTotal.Inc()

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

	candidate := core.Node{
		ID:         uuid.New(),
		Namespace:  h.cfg.ID,
		Labels:     req.Labels,
		Properties: props,
		Vector:     req.Vector,
		ModelID:    req.ModelID,
		Confidence: confidence,
		ValidFrom:  validFrom,
		TxTime:     time.Now(),
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
		if vvi, ok := h.db.vecs.(*memstore.VectorIndex); ok {
			vvi.RegisterNode(candidate)
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

	h.db.logger.Debug("write admitted",
		"namespace", h.cfg.ID,
		"node_id", candidate.ID,
		"source", req.SourceID,
		"confidence", candidate.Confidence)

	return WriteResult{
		NodeID:   candidate.ID,
		Admitted: true,
	}, nil
}

// ── Read path ─────────────────────────────────────────────────────────────────

// RetrieveRequest describes a single retrieval operation.
type RetrieveRequest struct {
	// Vector is the query embedding. Required for vector search.
	Vector []float32

	// SeedIDs are known relevant node IDs for graph traversal.
	// Optional — if empty, only vector search is used.
	SeedIDs []uuid.UUID

	// TopK is the maximum number of results to return. Default: 10.
	TopK int

	// ScoreParams overrides the namespace default scoring strategy.
	// Zero value uses namespace defaults.
	ScoreParams core.ScoreParams

	// Strategy overrides the namespace default retrieval strategy.
	Strategy retrieval.HybridStrategy

	// AsOf pins retrieval to a historical time (temporal query).
	// Zero value = now.
	AsOf time.Time
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

	// RetrievalSource indicates which path(s) found this node.
	RetrievalSource string
}

// Retrieve runs a hybrid retrieval query against the namespace.
func (h *NamespaceHandle) Retrieve(ctx context.Context, req RetrieveRequest) ([]Result, error) {
	start := time.Now()
	h.db.metrics.RetrievalTotal.Inc()

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
		Namespace:   h.cfg.ID,
		Vector:      req.Vector,
		SeedIDs:     req.SeedIDs,
		TopK:        topK,
		Strategy:    strategy,
		ScoreParams: params,
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

// ── Helpers ───────────────────────────────────────────────────────────────────

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
