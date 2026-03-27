package federation

import (
	"context"
	"log/slog"
	"time"

	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/internal/store/remote"
)

// Replicator periodically pulls missing events from peers.
type Replicator struct {
	federation *Federation
	applier    *Applier
	logger     *slog.Logger
}

// NewReplicator creates a replication loop bound to the given Federation.
func NewReplicator(f *Federation, applier *Applier, logger *slog.Logger) *Replicator {
	return &Replicator{federation: f, applier: applier, logger: logger}
}

// Run starts the pull loop. Blocks until ctx is cancelled.
func (r *Replicator) Run(ctx context.Context) {
	ticker := time.NewTicker(r.federation.config.PullInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pullAll(ctx)
		}
	}
}

func (r *Replicator) pullAll(ctx context.Context) {
	peers := r.federation.peers.AlivePeers()
	for _, peer := range peers {
		if err := r.pullFromPeer(ctx, peer); err != nil {
			r.logger.Warn("pull failed", "peer", peer.ID, "error", err)
			r.federation.metrics.SyncErrors.Add(1)
		}
	}
}

func (r *Replicator) pullFromPeer(ctx context.Context, peer *Peer) error {
	localWatermarks := r.federation.getWatermarksAsTime()

	for ns, peerWM := range peer.Watermarks {
		if !r.federation.shouldFederate(ns) {
			continue
		}

		localWM, ok := localWatermarks[ns]
		if !ok {
			localWM = time.Time{} // never synced this namespace
		}

		// Apply a one-minute safety margin for clock skew.
		pullFrom := localWM.Add(-time.Minute)

		if !peerWM.After(pullFrom) {
			continue // peer is not ahead of our local watermark
		}

		events, err := r.pullEvents(ctx, peer.GRPCAddr, ns, pullFrom)
		if err != nil {
			return err
		}

		if len(events) == 0 {
			continue
		}

		applied, err := r.applier.ApplyBatch(ctx, events)
		if err != nil {
			return err
		}

		r.federation.metrics.EventsReplicated.Add(int64(applied))
		r.federation.metrics.EventsSkipped.Add(int64(len(events) - applied))

		// Advance local watermark to the latest TxTime seen in this batch.
		for _, evt := range events {
			if evt.TxTime.After(localWM) {
				localWM = evt.TxTime
			}
		}
		r.federation.UpdateWatermark(ns, localWM)

		r.logger.Debug("replicated events",
			"peer", peer.ID,
			"namespace", ns,
			"pulled", len(events),
			"applied", applied)
	}
	return nil
}

// pullEvents dials the peer and calls EventSinceAll to retrieve events for a
// single namespace after the given timestamp.
func (r *Replicator) pullEvents(ctx context.Context, grpcAddr string, ns string, after time.Time) ([]store.Event, error) {
	client, err := remote.NewClient(ctx, grpcAddr)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.EventLog().SinceAll(ctx, ns, after)
}
