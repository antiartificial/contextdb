package federation

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// AntiEntropy performs periodic full reconciliation between peers.
// It checks for events that may have been missed by the normal pull loop
// (due to clock skew, gossip drops, etc.).
type AntiEntropy struct {
	federation *Federation
	applier    *Applier
	logger     *slog.Logger
}

// NewAntiEntropy creates an anti-entropy reconciler.
func NewAntiEntropy(f *Federation, applier *Applier, logger *slog.Logger) *AntiEntropy {
	return &AntiEntropy{federation: f, applier: applier, logger: logger}
}

// Run starts the anti-entropy loop. Blocks until ctx is cancelled.
func (ae *AntiEntropy) Run(ctx context.Context) {
	ticker := time.NewTicker(ae.federation.config.AntiEntropyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ae.reconcile(ctx)
		}
	}
}

func (ae *AntiEntropy) reconcile(ctx context.Context) {
	peers := ae.federation.peers.AlivePeers()
	if len(peers) == 0 {
		return
	}

	// For each namespace we federate, check the last hour's events
	// against each peer.
	window := time.Hour
	now := time.Now()
	since := now.Add(-window)

	for _, peer := range peers {
		for ns, peerWM := range peer.Watermarks {
			if !ae.federation.shouldFederate(ns) {
				continue
			}

			// Get our local events for this window.
			localEvents, err := ae.federation.log.SinceAll(ctx, ns, since)
			if err != nil {
				ae.logger.Warn("anti-entropy: failed to read local events",
					"namespace", ns, "error", err)
				continue
			}

			localIDs := make(map[uuid.UUID]bool, len(localEvents))
			for _, e := range localEvents {
				localIDs[e.ID] = true
			}

			// Pull peer's events for the same window.
			// Use the peer watermark as a hint — if peer has data in this window
			// skip if the peer's watermark predates the window entirely.
			if peerWM.Before(since) {
				continue // peer has no data in this window
			}

			// Note: actual pull would use gRPC to the peer. For now,
			// this is handled by the Replicator. Anti-entropy's job is
			// to detect divergence and trigger a catch-up pull with an
			// extended lookback window.

			// Trigger a pull with the extended lookback by rolling back
			// the local watermark for the namespace.
			ae.federation.UpdateWatermark(ns, since)

			ae.logger.Debug("anti-entropy triggered catchup",
				"peer", peer.ID,
				"namespace", ns,
				"local_events", len(localEvents))
		}
	}
}
