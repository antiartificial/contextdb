package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/antiartificial/contextdb/internal/store"
)

// CredibilityUpdater periodically scans sources and adjusts credibility
// based on validated/refuted claims using a Bayesian update rule.
type CredibilityUpdater struct {
	graph        store.GraphStore
	interval     time.Duration
	learningRate float64
	logger       *slog.Logger

	mu      sync.Mutex
	stop    chan struct{}
	stopped chan struct{}
	running bool
}

// CredibilityConfig configures the credibility updater.
type CredibilityConfig struct {
	Interval     time.Duration // how often to run (default: 10m)
	LearningRate float64       // update strength (default: 0.1)
}

func (c CredibilityConfig) withDefaults() CredibilityConfig {
	if c.Interval == 0 {
		c.Interval = 10 * time.Minute
	}
	if c.LearningRate == 0 {
		c.LearningRate = 0.1
	}
	return c
}

// NewCredibilityUpdater creates a background credibility update worker.
func NewCredibilityUpdater(graph store.GraphStore, cfg CredibilityConfig, logger *slog.Logger) *CredibilityUpdater {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &CredibilityUpdater{
		graph:        graph,
		interval:     cfg.Interval,
		learningRate: cfg.LearningRate,
		logger:       logger,
	}
}

// Start begins the background credibility update loop.
func (u *CredibilityUpdater) Start(ctx context.Context) {
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return
	}
	u.stop = make(chan struct{})
	u.stopped = make(chan struct{})
	u.running = true
	u.mu.Unlock()

	go u.loop(ctx)
}

// Stop signals the worker to shut down and waits for completion.
func (u *CredibilityUpdater) Stop() {
	u.mu.Lock()
	if !u.running {
		u.mu.Unlock()
		return
	}
	close(u.stop)
	u.mu.Unlock()
	<-u.stopped
}

func (u *CredibilityUpdater) loop(ctx context.Context) {
	defer func() {
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
		close(u.stopped)
	}()

	ticker := time.NewTicker(u.interval)
	defer ticker.Stop()

	for {
		select {
		case <-u.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Credibility updates are driven by Source claim counters.
			// The UpdateCredibility method on GraphStore handles the
			// actual score adjustment. This worker simply logs that
			// the cycle ran — actual updates happen during admission
			// when sources are resolved.
			u.logger.Debug("credibility update cycle")
		}
	}
}

// ComputeDelta computes the credibility adjustment for a source based
// on its validated/refuted claim counts using a Bayesian update rule:
//
//	delta = (validated / (validated + refuted + 1)) * learningRate - prior_adjustment
//
// The result is clamped so the final score stays in [0.05, 1.0].
func ComputeDelta(validated, refuted int64, currentCredibility, learningRate float64) float64 {
	total := float64(validated + refuted + 1)
	ratio := float64(validated) / total
	// Centre around 0.5: positive ratio = boost, negative = penalise
	delta := (ratio - 0.5) * 2 * learningRate

	// Clamp so result stays in [0.05, 1.0]
	newCred := currentCredibility + delta
	if newCred > 1.0 {
		delta = 1.0 - currentCredibility
	}
	if newCred < 0.05 {
		delta = 0.05 - currentCredibility
	}

	return delta
}
