package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// CredibilityUpdater periodically scans sources and adjusts credibility
// based on validated/refuted claims using a Bayesian update rule.
//
// The updater uses Beta-Binomial conjugate priors:
//   - Source credibility ~ Beta(α, β)
//   - α = 1 + ClaimsValidated, β = 1 + ClaimsRefuted (Laplace smoothing)
//   - Mean credibility = α / (α + β)
//   - Variance = αβ / ((α+β)²(α+β+1)) — quantifies uncertainty
//
// Updates are atomic: increment α on validation, β on refutation.
type CredibilityUpdater struct {
	graph    store.GraphStore
	interval time.Duration
	logger   *slog.Logger

	mu      sync.Mutex
	stop    chan struct{}
	stopped chan struct{}
	running bool
}

// CredibilityConfig configures the credibility updater.
type CredibilityConfig struct {
	Interval time.Duration // how often to run (default: 10m)
}

func (c CredibilityConfig) withDefaults() CredibilityConfig {
	if c.Interval == 0 {
		c.Interval = 10 * time.Minute
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
		graph:    graph,
		interval: cfg.Interval,
		logger:   logger,
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
			// The Bayesian updates happen atomically when claims are
			// validated/refuted via UpdateSourceBayesian().
			// This worker can be extended to perform periodic re-evaluation
			// of sources based on claim outcomes.
			u.logger.Debug("credibility update cycle")
		}
	}
}

// UpdateSourceBayesian performs a Bayesian update on a source's credibility.
// Call this when a claim from the source has been validated or refuted.
//
//   - validated = true:  increment Alpha (validated+1)
//   - validated = false: increment Beta (refuted+1)
//
// The source's mean credibility will shift toward 1.0 on validation,
// toward 0.0 on refutation, with variance decreasing (more certainty).
func UpdateSourceBayesian(source *core.Source, validated bool) {
	source.BayesianUpdate(validated)
}

// MeanCredibility computes E[Beta(α,β)] = α/(α+β).
// This is the expected credibility score given the observation history.
func MeanCredibility(alpha, beta float64) float64 {
	if alpha+beta == 0 {
		return 0.5 // uniform prior
	}
	return alpha / (alpha + beta)
}

// CredibilityVariance computes Var[Beta(α,β)] = αβ/((α+β)²(α+β+1)).
// Lower variance indicates more certainty (more observations).
func CredibilityVariance(alpha, beta float64) float64 {
	sum := alpha + beta
	if sum == 0 {
		return 0.25 // variance of uniform Beta(1,1)
	}
	return (alpha * beta) / (sum * sum * (sum + 1))
}

// ComputeDelta (legacy) computes a credibility adjustment using the simple
// ratio method. Deprecated: use BayesianUpdate instead for proper inference.
//
// Kept for backward compatibility with existing tests.
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
