package client

import (
	"context"
	"fmt"
	"github.com/antiartificial/contextdb/internal/embedding"
	"github.com/antiartificial/contextdb/internal/extract"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	badgerstore "github.com/antiartificial/contextdb/internal/store/badger"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
	pgstore "github.com/antiartificial/contextdb/internal/store/postgres"
	remotestore "github.com/antiartificial/contextdb/internal/store/remote"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Mode selects the storage backend.
type Mode string

const (
	// ModeEmbedded runs entirely in-process. No external dependencies.
	// Suitable for development, testing, and sidecar deployments.
	ModeEmbedded Mode = "embedded"

	// ModeStandard connects to Postgres with the pgvector extension.
	// Set Options.DSN to the connection string.
	// A non-empty DSN is required; connection errors are returned.
	ModeStandard Mode = "standard"

	// ModeRemote connects to a running contextdb server over HTTP.
	// Set Options.Addr to the server address.
	ModeRemote Mode = "remote"

	// ModeScaled is reserved for Qdrant/Redis/Postgres deployments.
	// This build does not wire those stores together and rejects the mode.
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
// Configuration is validated and backend connections are established before return.
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
		if strings.TrimSpace(db.opts.DSN) == "" {
			return fmt.Errorf("standard mode requires a Postgres DSN; use embedded mode explicitly for local storage")
		}
		return db.connectPostgres()

	case ModeRemote:
		return db.connectRemote()

	case ModeScaled:
		return fmt.Errorf("scaled mode is not supported by this build; use standard mode for Postgres/pgvector")

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

// NamespaceHandle is scoped to a single namespace. All reads and writes
// through this handle are isolated from other namespaces.
type NamespaceHandle struct {
	db               *DB
	cfg              namespace.Config
	engine           *retrieval.Engine
	acquisitionMu    sync.Mutex
	reviewMu         sync.Mutex
	idempotencyMu    sync.Mutex
	idempotencyLocks map[string]*idempotencyLock
}
