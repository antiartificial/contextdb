package federation

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"

	"github.com/antiartificial/contextdb/internal/store"
)

// Federation coordinates gossip, replication, and anti-entropy across peers.
type Federation struct {
	config Config
	peerID string
	list   *memberlist.Memberlist
	peers  *PeerRegistry
	graph  store.GraphStore
	vecs   store.VectorIndex
	log    store.EventLog
	logger *slog.Logger

	mu         sync.RWMutex
	watermarks map[string]time.Time // local ns → latest TxTime

	metrics Metrics

	cancel context.CancelFunc
	done   chan struct{}
}

// New creates a new Federation instance.
// graph, vecs, and eventLog may be nil for unit-test scenarios.
func New(
	graph store.GraphStore,
	vecs store.VectorIndex,
	eventLog store.EventLog,
	cfg Config,
	logger *slog.Logger,
) *Federation {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Federation{
		config:     cfg,
		peerID:     cfg.PeerID,
		peers:      NewPeerRegistry(),
		graph:      graph,
		vecs:       vecs,
		log:        eventLog,
		logger:     logger,
		watermarks: make(map[string]time.Time),
	}
}

// Start initialises memberlist and begins background loops.
// It is non-blocking; federation runs until Stop is called.
func (f *Federation) Start(ctx context.Context) error {
	mlConfig := memberlist.DefaultLANConfig()
	mlConfig.Name = f.peerID
	mlConfig.Delegate = &federationDelegate{federation: f, logger: f.logger}
	mlConfig.Events = &federationEvents{federation: f, logger: f.logger}
	mlConfig.LogOutput = &discardLogger{} // suppress memberlist's own logger

	// Parse host/port from the configured bind address.
	host, portStr, err := net.SplitHostPort(f.config.BindAddr)
	if err != nil {
		return fmt.Errorf("federation: invalid BindAddr %q: %w", f.config.BindAddr, err)
	}
	if host != "" {
		mlConfig.BindAddr = host
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("federation: invalid port in BindAddr %q: %w", f.config.BindAddr, err)
	}
	mlConfig.BindPort = port

	list, err := memberlist.Create(mlConfig)
	if err != nil {
		return fmt.Errorf("federation: create memberlist: %w", err)
	}
	f.list = list

	// Join seed peers (non-fatal — we might be the first node).
	if len(f.config.SeedPeers) > 0 {
		if _, err := list.Join(f.config.SeedPeers); err != nil {
			f.logger.Warn("failed to join seed peers", "error", err)
		}
	}

	ctx, f.cancel = context.WithCancel(ctx)
	f.done = make(chan struct{})

	// Start the pull replication loop.
	applier := NewApplier(f.graph, f.vecs, f.log, f.peerID)
	replicator := NewReplicator(f, applier, f.logger)
	go func() {
		defer close(f.done)
		replicator.Run(ctx)
	}()

	f.logger.Info("federation started",
		"peer_id", f.peerID,
		"bind", f.config.BindAddr,
		"seeds", f.config.SeedPeers,
	)

	return nil
}

// Stop gracefully shuts down federation and leaves the cluster.
func (f *Federation) Stop() {
	if f.cancel != nil {
		f.cancel()
	}
	if f.list != nil {
		_ = f.list.Leave(5 * time.Second)
		_ = f.list.Shutdown()
	}
	f.logger.Info("federation stopped", "peer_id", f.peerID)
}

// PeerID returns this instance's stable peer identifier.
func (f *Federation) PeerID() string {
	return f.peerID
}

// Peers returns the live peer registry.
func (f *Federation) Peers() *PeerRegistry {
	return f.peers
}

// UpdateWatermark sets the high-water-mark for a namespace.
// The watermark only advances forward; older timestamps are ignored.
func (f *Federation) UpdateWatermark(ns string, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.watermarks[ns]; !ok || t.After(existing) {
		f.watermarks[ns] = t
	}
}

// getWatermarks returns a snapshot of the current namespace watermarks as
// UnixNano values, suitable for inclusion in NodeMeta gossip payloads.
func (f *Federation) getWatermarks() map[string]int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make(map[string]int64, len(f.watermarks))
	for ns, t := range f.watermarks {
		result[ns] = t.UnixNano()
	}
	return result
}

// getWatermarksAsTime returns a snapshot of the current namespace watermarks
// as time.Time values. Used by the replicator to determine pull boundaries.
func (f *Federation) getWatermarksAsTime() map[string]time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make(map[string]time.Time, len(f.watermarks))
	for ns, t := range f.watermarks {
		result[ns] = t
	}
	return result
}

// shouldFederate reports whether the given namespace should be replicated.
// If config.Namespaces is empty, all namespaces are federated.
// A wildcard entry "*" also matches any namespace.
func (f *Federation) shouldFederate(ns string) bool {
	if len(f.config.Namespaces) == 0 {
		return true // federate all
	}
	for _, n := range f.config.Namespaces {
		if n == "*" || n == ns {
			return true
		}
	}
	return false
}

// discardLogger satisfies the io.Writer interface required by memberlist's
// LogOutput field, silently discarding all log output.
type discardLogger struct{}

func (d *discardLogger) Write(p []byte) (n int, err error) { return len(p), nil }
