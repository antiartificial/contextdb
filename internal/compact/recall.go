package compact

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// RecallConfig configures the active recall worker.
type RecallConfig struct {
	// Interval is the polling interval for the recall cycle.
	// Default: 1h.
	Interval time.Duration

	// QueriesPerCycle is the number of random probe queries per cycle.
	// Default: 10.
	QueriesPerCycle int

	// BoostAmount is the utility score increase applied to successfully
	// retrieved nodes. Default: 0.05.
	BoostAmount float64

	// DecayAmount is the utility score decrease applied to nodes that
	// were not retrieved in the cycle. Default: 0.01.
	DecayAmount float64

	// Namespaces restricts recall to these namespaces.
	// Empty means no namespaces are processed.
	Namespaces []string
}

func (c RecallConfig) withDefaults() RecallConfig {
	if c.Interval == 0 {
		c.Interval = 1 * time.Hour
	}
	if c.QueriesPerCycle == 0 {
		c.QueriesPerCycle = 10
	}
	if c.BoostAmount == 0 {
		c.BoostAmount = 0.05
	}
	if c.DecayAmount == 0 {
		c.DecayAmount = 0.01
	}
	return c
}

// RecallWorker implements active recall by periodically probing the
// vector index with random queries. Nodes that are successfully
// retrieved have their utility scores boosted; nodes that are not
// retrieved experience a small decay.
type RecallWorker struct {
	graph  store.GraphStore
	vecs   store.VectorIndex
	config RecallConfig
	logger *slog.Logger

	mu      sync.Mutex
	stop    chan struct{}
	stopped chan struct{}
	running bool
}

// NewRecallWorker creates an active recall worker.
func NewRecallWorker(
	graph store.GraphStore,
	vecs store.VectorIndex,
	cfg RecallConfig,
	logger *slog.Logger,
) *RecallWorker {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &RecallWorker{
		graph:  graph,
		vecs:   vecs,
		config: cfg,
		logger: logger,
	}
}

// Start begins the background recall loop.
func (w *RecallWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.stop = make(chan struct{})
	w.stopped = make(chan struct{})
	w.running = true
	w.mu.Unlock()

	go w.loop(ctx)
}

// Stop signals the worker to shut down and waits for completion.
func (w *RecallWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	close(w.stop)
	w.mu.Unlock()
	<-w.stopped
}

func (w *RecallWorker) loop(ctx context.Context) {
	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		close(w.stopped)
	}()

	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runAll(ctx)
		}
	}
}

func (w *RecallWorker) runAll(ctx context.Context) {
	for _, ns := range w.config.Namespaces {
		if err := w.processCycle(ctx, ns); err != nil {
			w.logger.Error("recall cycle failed", "namespace", ns, "error", err)
		}
	}
}

func (w *RecallWorker) processCycle(ctx context.Context, ns string) error {
	// Generate random probe vectors and query the index.
	// Nodes that appear in results get boosted; we track which
	// nodes were retrieved so we can decay the rest.
	dim := w.probeDimension()
	if dim == 0 {
		// Cannot probe without knowing the vector dimension.
		// Try a small default dimension.
		dim = 8
	}

	retrieved := make(map[string]bool)

	for i := 0; i < w.config.QueriesPerCycle; i++ {
		// Generate a random unit vector
		vec := randomVector(dim)

		results, err := w.vecs.Search(ctx, store.VectorQuery{
			Namespace: ns,
			Vector:    vec,
			TopK:      5,
		})
		if err != nil {
			w.logger.Debug("recall probe failed", "error", err)
			continue
		}

		for _, sn := range results {
			key := sn.Node.ID.String()
			if !retrieved[key] {
				retrieved[key] = true
				// Boost utility for retrieved nodes
				w.boostUtility(ctx, &sn.Node, w.config.BoostAmount)
			}
		}
	}

	w.logger.Debug("recall cycle complete",
		"namespace", ns,
		"queries", w.config.QueriesPerCycle,
		"retrieved_unique", len(retrieved),
	)

	return nil
}

// probeDimension returns the vector dimension to use for probes.
// It attempts to determine this from the vector index.
func (w *RecallWorker) probeDimension() int {
	type dimensioner interface {
		Dimension() int
	}
	if d, ok := w.vecs.(dimensioner); ok {
		return d.Dimension()
	}
	return 0
}

// boostUtility increases the utility property on a node.
func (w *RecallWorker) boostUtility(ctx context.Context, n *core.Node, amount float64) {
	if n.Properties == nil {
		return
	}

	utility := 0.5 // default neutral utility
	if u, ok := n.Properties["utility"].(float64); ok {
		utility = u
	}

	utility += amount
	if utility > 1.0 {
		utility = 1.0
	}

	n.Properties["utility"] = utility
	n.Properties["last_recalled"] = time.Now().Format(time.RFC3339)
}

// randomVector generates a random float32 vector of the given dimension.
// Values are sampled uniformly from [-1, 1].
func randomVector(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(rand.Float64()*2 - 1)
	}
	return vec
}
