package compact

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// RecallConfig configures the active recall (SM-2 spaced repetition) worker.
type RecallConfig struct {
	// Interval is the polling interval for the recall cycle.
	// Default: 1h.
	Interval time.Duration

	// MaxReviewsPerCycle is the maximum number of due memories to review
	// per cycle. Default: 20.
	MaxReviewsPerCycle int

	// Namespaces restricts recall to these namespaces.
	// Empty means no namespaces are processed.
	Namespaces []string
}

func (c RecallConfig) withDefaults() RecallConfig {
	if c.Interval == 0 {
		c.Interval = 1 * time.Hour
	}
	if c.MaxReviewsPerCycle == 0 {
		c.MaxReviewsPerCycle = 20
	}
	return c
}

// RecallWorker implements SM-2 spaced repetition for memory nodes.
// It periodically scans for due memories, simulates retrieval attempts,
// and updates SM-2 parameters (easiness factor, interval, next review date).
//
// The SM-2 algorithm optimizes memory retention by scheduling reviews
// at increasing intervals for easy items and shorter intervals for
// difficult items, maximizing long-term retention with minimal effort.
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

// NewRecallWorker creates an active recall worker implementing SM-2.
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

// processCycle runs the SM-2 active recall for due memories.
// It finds memories with NextReviewDate <= now, simulates retrieval quality
// based on current node state, and updates SM-2 scheduling parameters.
func (w *RecallWorker) processCycle(ctx context.Context, ns string) error {
	now := time.Now()

	// Find due memories by walking the graph for nodes with SM-2 data
	// that are scheduled for review
	dueNodes, err := w.findDueMemories(ctx, ns, now)
	if err != nil {
		return err
	}

	if len(dueNodes) == 0 {
		w.logger.Debug("no memories due for review", "namespace", ns)
		return nil
	}

	// Limit reviews per cycle
	if len(dueNodes) > w.config.MaxReviewsPerCycle {
		dueNodes = dueNodes[:w.config.MaxReviewsPerCycle]
	}

	reviewed := 0
	for _, node := range dueNodes {
		if err := w.reviewMemory(ctx, node, now); err != nil {
			w.logger.Error("review failed", "node_id", node.ID, "error", err)
			continue
		}
		reviewed++
	}

	w.logger.Info("recall cycle complete",
		"namespace", ns,
		"due_count", len(dueNodes),
		"reviewed", reviewed,
	)

	return nil
}

// findDueMemories scans for nodes with due SM-2 review dates.
// Uses graph walk to find candidate memories, then filters by SM-2 state.
func (w *RecallWorker) findDueMemories(ctx context.Context, ns string, now time.Time) ([]core.Node, error) {
	// Get all nodes in namespace - in production this would use a more
	// targeted query. For now, walk from a nil seed to get all nodes.
	var allNodes []core.Node

	// Try to get nodes via graph walk with high depth
	if w.graph != nil {
		// Get a sample of recent nodes by walking with empty seeds
		// This is a simplified approach - production would use a proper query
		walked, err := w.graph.Walk(ctx, store.WalkQuery{
			Namespace: ns,
			SeedIDs:   []uuid.UUID{},
			MaxDepth:  5,
			Strategy:  store.StrategyBFS,
			AsOf:      now,
		})
		if err == nil {
			allNodes = walked
		}
	}

	// Filter for nodes with SM-2 data that are due
	var due []core.Node
	for _, node := range allNodes {
		sm2 := core.Sm2FromProperties(node.Properties)
		if sm2.IsDue(now) {
			due = append(due, node)
		}
	}

	return due, nil
}

// reviewMemory performs an SM-2 review on a memory node.
// It simulates retrieval quality based on the node's current state,
// then updates the SM-2 parameters accordingly.
func (w *RecallWorker) reviewMemory(ctx context.Context, node core.Node, now time.Time) error {
	sm2 := core.Sm2FromProperties(node.Properties)

	// Simulate retrieval quality based on node properties
	// Quality 0-5 based on: confidence, recency, utility
	quality := w.simulateRetrievalQuality(node)

	// Apply SM-2 update
	newSM2 := sm2.Update(quality)

	// Persist updated SM-2 data back to node properties
	node.Properties = newSM2.ToProperties(node.Properties)

	// Update utility score based on retrieval success
	// Success (quality >= 3) boosts utility, failure reduces it
	utility := w.computeUtility(node, quality)
	node.Properties["utility"] = utility

	// Persist node changes
	if err := w.graph.UpsertNode(ctx, node); err != nil {
		return err
	}

	w.logger.Debug("memory reviewed",
		"node_id", node.ID,
		"quality", quality,
		"new_interval", newSM2.IntervalDays,
		"new_ef", newSM2.EasinessFactor,
		"next_review", newSM2.NextReviewDate.Format(time.RFC3339),
	)

	return nil
}

// simulateRetrievalQuality generates a quality rating (0-5) based on
// the node's current state: confidence, recency, and utility.
// This simulates how well the memory would be recalled.
func (w *RecallWorker) simulateRetrievalQuality(node core.Node) int {
	// Base quality on node confidence
	confidence := node.Confidence
	if confidence == 0 {
		confidence = 0.5
	}

	// Get utility if available
	utility := 0.5
	if u, ok := node.Properties["utility"].(float64); ok {
		utility = u
	}

	// Calculate recency factor
	age := time.Since(node.ValidFrom).Hours()
	recency := math.Exp(-0.05 * age) // exponential decay

	// Combined score
	score := 0.4*confidence + 0.3*utility + 0.3*recency

	// Map to quality rating 0-5
	// 0.9+ → 5 (perfect)
	// 0.7-0.9 → 4 (good)
	// 0.5-0.7 → 3 (pass)
	// 0.3-0.5 → 2 (fail, seemed easy)
	// 0.1-0.3 → 1 (fail, remembered)
	// <0.1 → 0 (blackout)
	switch {
	case score >= 0.9:
		return 5
	case score >= 0.7:
		return 4
	case score >= 0.5:
		return 3
	case score >= 0.3:
		return 2
	case score >= 0.1:
		return 1
	default:
		return 0
	}
}

// computeUtility updates the utility score based on retrieval quality.
func (w *RecallWorker) computeUtility(node core.Node, quality int) float64 {
	current := 0.5
	if u, ok := node.Properties["utility"].(float64); ok {
		current = u
	}

	if quality >= 3 {
		// Successful recall: boost utility
		current += 0.05 * float64(quality) / 5.0
	} else {
		// Failed recall: reduce utility
		current -= 0.02 * float64(3-quality)
	}

	// Clamp to [0, 1]
	if current > 1.0 {
		return 1.0
	}
	if current < 0 {
		return 0
	}
	return current
}
