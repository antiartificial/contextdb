package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// Applier replays remote events into the local store with conflict resolution.
type Applier struct {
	graph  store.GraphStore
	vecs   store.VectorIndex
	log    store.EventLog
	peerID string // local peer ID, to skip echoed events
}

// NewApplier creates an event applier for federation replication.
func NewApplier(graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, peerID string) *Applier {
	return &Applier{graph: graph, vecs: vecs, log: log, peerID: peerID}
}

// ApplyBatch replays a batch of remote events, handling conflicts via LWW.
// Events are sorted by TxTime before application. Returns the number of
// events applied (vs skipped due to conflict resolution).
func (a *Applier) ApplyBatch(ctx context.Context, events []store.Event) (applied int, err error) {
	// Sort by TxTime for causal ordering.
	sort.Slice(events, func(i, j int) bool {
		return events[i].TxTime.Before(events[j].TxTime)
	})

	for _, evt := range events {
		// Skip events that originated from us (prevent echo).
		if evt.Origin == a.peerID {
			continue
		}

		ok, err := a.applyOne(ctx, evt)
		if err != nil {
			return applied, fmt.Errorf("apply event %s: %w", evt.ID, err)
		}
		if ok {
			applied++
		}
	}
	return applied, nil
}

func (a *Applier) applyOne(ctx context.Context, evt store.Event) (bool, error) {
	switch evt.Type {
	case store.EventNodeUpsert:
		return a.applyNodeUpsert(ctx, evt)
	case store.EventEdgeUpsert:
		return a.applyEdgeUpsert(ctx, evt)
	case store.EventEdgeInvalidate:
		return a.applyEdgeInvalidate(ctx, evt)
	case store.EventNodeRetract:
		return a.applyNodeRetract(ctx, evt)
	case store.EventSourceUpdate:
		return a.applySourceUpdate(ctx, evt)
	default:
		return false, nil // unknown event type, skip
	}
}

func (a *Applier) applyNodeUpsert(ctx context.Context, evt store.Event) (bool, error) {
	var remote core.Node
	if err := json.Unmarshal(evt.Payload, &remote); err != nil {
		return false, err
	}

	local, err := a.graph.GetNode(ctx, remote.Namespace, remote.ID)
	if err != nil {
		return false, err
	}

	if local != nil {
		// LWW: higher Version wins. On tie, later TxTime wins. On TxTime tie, higher PeerID wins.
		if remote.Version < local.Version {
			return false, nil // local is ahead
		}
		if remote.Version == local.Version {
			if remote.TxTime.Before(local.TxTime) {
				return false, nil // local TxTime is newer
			}
			if remote.TxTime.Equal(local.TxTime) && evt.Origin <= a.peerID {
				return false, nil // tie-break by PeerID
			}
		}
	}

	if err := a.graph.UpsertNode(ctx, remote); err != nil {
		return false, err
	}

	// Log the replicated event locally (non-fatal on failure — the node was written).
	if err := a.log.Append(ctx, evt); err != nil {
		_ = err
	}
	return true, nil
}

func (a *Applier) applyEdgeUpsert(ctx context.Context, evt store.Event) (bool, error) {
	var remote core.Edge
	if err := json.Unmarshal(evt.Payload, &remote); err != nil {
		return false, err
	}
	// Edges are idempotent — just upsert.
	if err := a.graph.UpsertEdge(ctx, remote); err != nil {
		return false, err
	}
	return true, nil
}

func (a *Applier) applyEdgeInvalidate(ctx context.Context, evt store.Event) (bool, error) {
	var payload struct {
		Namespace string    `json:"namespace"`
		ID        uuid.UUID `json:"id"`
		At        time.Time `json:"at"`
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return false, err
	}
	if err := a.graph.InvalidateEdge(ctx, payload.Namespace, payload.ID, payload.At); err != nil {
		return false, err
	}
	return true, nil
}

func (a *Applier) applyNodeRetract(ctx context.Context, evt store.Event) (bool, error) {
	var payload struct {
		Namespace string    `json:"namespace"`
		ID        uuid.UUID `json:"id"`
		Reason    string    `json:"reason"`
		At        time.Time `json:"at"`
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return false, err
	}
	if err := a.graph.RetractNode(ctx, payload.Namespace, payload.ID, payload.Reason, payload.At); err != nil {
		// May already be retracted — non-fatal.
		_ = err
	}
	return true, nil
}

func (a *Applier) applySourceUpdate(ctx context.Context, evt store.Event) (bool, error) {
	var remote core.Source
	if err := json.Unmarshal(evt.Payload, &remote); err != nil {
		return false, err
	}

	local, err := a.graph.GetSourceByExternalID(ctx, remote.Namespace, remote.ExternalID)
	if err != nil || local == nil {
		// New source — just upsert.
		return true, a.graph.UpsertSource(ctx, remote)
	}

	// Additive merge in Beta space: sum observations, subtract shared Beta(1,1) prior.
	local.Alpha += remote.Alpha - 1
	local.Beta += remote.Beta - 1
	if local.Alpha < 1 {
		local.Alpha = 1
	}
	if local.Beta < 1 {
		local.Beta = 1
	}
	local.ClaimsAsserted += remote.ClaimsAsserted
	local.ClaimsValidated += remote.ClaimsValidated
	local.ClaimsRefuted += remote.ClaimsRefuted
	if remote.UpdatedAt.After(local.UpdatedAt) {
		local.UpdatedAt = remote.UpdatedAt
	}

	return true, a.graph.UpsertSource(ctx, *local)
}
