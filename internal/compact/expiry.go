package compact

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/store"
)

// ExpiryAlert signals that a node is approaching or has passed its expiry.
type ExpiryAlert struct {
	NodeID     uuid.UUID
	Namespace  string
	ExpiresAt  time.Time
	Confidence float64
	Urgency    string // "imminent" (< 24h), "approaching" (< 7d), "expired"
}

// ExpiryConfig configures the expiry notification worker.
type ExpiryConfig struct {
	// Interval between scans. Default: 1 hour.
	Interval time.Duration
	// Namespaces to scan. Nil = none scanned (must be explicitly configured).
	Namespaces []string
	// Horizon is how far ahead to look for upcoming expiries. Default: 7 days.
	Horizon time.Duration
}

func (c ExpiryConfig) withDefaults() ExpiryConfig {
	if c.Interval == 0 {
		c.Interval = time.Hour
	}
	if c.Horizon == 0 {
		c.Horizon = 7 * 24 * time.Hour
	}
	return c
}

// ExpiryWorker scans for nodes approaching ValidUntil and accumulates
// ExpiryAlert values that callers can retrieve via Alerts.
type ExpiryWorker struct {
	graph  store.GraphStore
	config ExpiryConfig
	logger *slog.Logger

	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}

	alertsMu sync.Mutex
	alerts   []ExpiryAlert
}

// NewExpiryWorker creates a new expiry notification worker.
func NewExpiryWorker(graph store.GraphStore, config ExpiryConfig, log *slog.Logger) *ExpiryWorker {
	config = config.withDefaults()
	if log == nil {
		log = slog.Default()
	}
	return &ExpiryWorker{
		graph:  graph,
		config: config,
		logger: log,
	}
}

// Start begins the background scan loop.
func (w *ExpiryWorker) Start(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stop = make(chan struct{})
	w.done = make(chan struct{})
	go w.loop(ctx)
}

// Stop gracefully stops the worker and waits for the loop to exit.
func (w *ExpiryWorker) Stop() {
	w.mu.Lock()
	stop := w.stop
	done := w.done
	w.mu.Unlock()

	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// Alerts returns a snapshot of all current expiry alerts.
func (w *ExpiryWorker) Alerts() []ExpiryAlert {
	w.alertsMu.Lock()
	defer w.alertsMu.Unlock()
	result := make([]ExpiryAlert, len(w.alerts))
	copy(result, w.alerts)
	return result
}

func (w *ExpiryWorker) loop(ctx context.Context) {
	defer close(w.done)

	// Run immediately on start.
	w.scan(ctx)

	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan(ctx)
		}
	}
}

func (w *ExpiryWorker) scan(ctx context.Context) {
	now := time.Now()
	horizon := now.Add(w.config.Horizon)
	var newAlerts []ExpiryAlert

	for _, ns := range w.config.Namespaces {
		// Retrieve all nodes valid right now; we then inspect their ValidUntil.
		nodes, err := w.graph.ValidAt(ctx, ns, now, nil)
		if err != nil {
			w.logger.Error("expiry scan failed", "namespace", ns, "error", err)
			continue
		}

		for _, n := range nodes {
			if n.ValidUntil == nil {
				continue // no expiry set — skip
			}

			expiresAt := *n.ValidUntil

			var urgency string
			switch {
			case now.After(expiresAt):
				urgency = "expired"
			case expiresAt.Before(now.Add(24 * time.Hour)):
				urgency = "imminent"
			case expiresAt.Before(horizon):
				urgency = "approaching"
			default:
				continue // beyond the look-ahead horizon
			}

			newAlerts = append(newAlerts, ExpiryAlert{
				NodeID:     n.ID,
				Namespace:  ns,
				ExpiresAt:  expiresAt,
				Confidence: n.Confidence,
				Urgency:    urgency,
			})
		}
	}

	w.alertsMu.Lock()
	w.alerts = newAlerts
	w.alertsMu.Unlock()

	if len(newAlerts) > 0 {
		w.logger.Info("expiry scan complete",
			"alerts", len(newAlerts),
			"namespaces", len(w.config.Namespaces))
	}
}
