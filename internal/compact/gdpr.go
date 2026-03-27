package compact

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/store"
)

// ErasureRequest describes a GDPR right-to-erasure request.
type ErasureRequest struct {
	Namespace   string
	SourceID    string    // external source ID to erase (e.g. "user:123")
	Reason      string    // audit reason
	EffectiveAt time.Time // when the erasure takes effect (zero = now)
}

// ErasureReport is the audit trail for a GDPR erasure.
type ErasureReport struct {
	RequestedAt      time.Time
	CompletedAt      time.Time
	Namespace        string
	SourceID         string
	NodesRetracted   int
	VectorsDeleted   int
	EdgesInvalidated int
	EventsRedacted   int
	NodeIDs          []uuid.UUID
	Errors           []string
}

// GDPRProcessor orchestrates data erasure across all storage layers.
type GDPRProcessor struct {
	graph store.GraphStore
	vecs  store.VectorIndex
	kv    store.KVStore
	log   store.EventLog
}

// NewGDPRProcessor creates a GDPR processor.
func NewGDPRProcessor(graph store.GraphStore, vecs store.VectorIndex, kv store.KVStore, log store.EventLog) *GDPRProcessor {
	return &GDPRProcessor{graph: graph, vecs: vecs, kv: kv, log: log}
}

// ProcessErasure executes a GDPR erasure request across all storage layers.
// This is NOT deletion — it's retraction with audit trail. The graph structure
// (edges, version history) is preserved with retraction markers. Vector embeddings
// and KV cache entries are fully deleted.
func (p *GDPRProcessor) ProcessErasure(ctx context.Context, req ErasureRequest) (*ErasureReport, error) {
	if req.EffectiveAt.IsZero() {
		req.EffectiveAt = time.Now()
	}

	report := &ErasureReport{
		RequestedAt: time.Now(),
		Namespace:   req.Namespace,
		SourceID:    req.SourceID,
	}

	// Step 1: Find all nodes from this source.
	nodes, err := p.graph.ValidAt(ctx, req.Namespace, req.EffectiveAt, nil)
	if err != nil {
		return report, fmt.Errorf("gdpr: find nodes: %w", err)
	}

	var targetNodes []uuid.UUID
	for _, n := range nodes {
		sid, ok := n.Properties["source_id"].(string)
		if !ok || sid != req.SourceID {
			continue
		}
		targetNodes = append(targetNodes, n.ID)
	}

	// Step 2: Retract all target nodes.
	reason := fmt.Sprintf("GDPR erasure request: %s", req.Reason)
	for _, nodeID := range targetNodes {
		if err := p.graph.RetractNode(ctx, req.Namespace, nodeID, reason, req.EffectiveAt); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("retract %s: %v", nodeID, err))
			continue
		}
		report.NodesRetracted++
		report.NodeIDs = append(report.NodeIDs, nodeID)
	}

	// Step 3: Delete vector embeddings.
	if p.vecs != nil {
		for _, nodeID := range targetNodes {
			if err := p.vecs.Delete(ctx, req.Namespace, nodeID); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("delete vector %s: %v", nodeID, err))
				continue
			}
			report.VectorsDeleted++
		}
	}

	// Build a set of target node IDs for fast lookup.
	targetSet := make(map[uuid.UUID]bool, len(targetNodes))
	for _, id := range targetNodes {
		targetSet[id] = true
	}

	// Step 4: Invalidate edges from/to target nodes.
	// Skip "retracted" self-edges that RetractNode itself created — those are
	// internal audit markers and are not subject to separate invalidation.
	for _, nodeID := range targetNodes {
		edges, err := p.graph.GetEdges(ctx, req.Namespace, nodeID)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e.Type == "retracted" && e.Src == e.Dst {
				continue // skip the self-retraction marker
			}
			if err := p.graph.InvalidateEdge(ctx, req.Namespace, e.ID, req.EffectiveAt); err != nil {
				continue
			}
			report.EdgesInvalidated++
		}
		// Also invalidate incoming edges.
		edgesTo, err := p.graph.GetEdgesTo(ctx, req.Namespace, nodeID)
		if err != nil {
			continue
		}
		for _, e := range edgesTo {
			if e.Type == "retracted" && e.Src == e.Dst {
				continue // skip the self-retraction marker
			}
			if err := p.graph.InvalidateEdge(ctx, req.Namespace, e.ID, req.EffectiveAt); err != nil {
				continue
			}
			report.EdgesInvalidated++
		}
	}

	// Step 5: Evict KV cache entries for this source (best-effort).
	if p.kv != nil {
		cacheKey := fmt.Sprintf("%s:source:%s", req.Namespace, req.SourceID)
		_ = p.kv.Delete(ctx, cacheKey)
	}

	report.CompletedAt = time.Now()
	return report, nil
}
