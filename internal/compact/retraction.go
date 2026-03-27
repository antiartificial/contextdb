package compact

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// RetractResult summarizes a retraction operation.
type RetractResult struct {
	NodesRetracted int
	CascadeDepth   int
	NodeIDs        []uuid.UUID
}

// BulkRetractor performs subject-based and cascade retractions.
type BulkRetractor struct {
	graph store.GraphStore
}

// NewBulkRetractor creates a retractor.
func NewBulkRetractor(graph store.GraphStore) *BulkRetractor {
	return &BulkRetractor{graph: graph}
}

// RetractBySource retracts all currently-valid nodes written by the given source.
func (r *BulkRetractor) RetractBySource(ctx context.Context, ns, sourceExternalID, reason string) (*RetractResult, error) {
	now := time.Now()
	nodes, err := r.graph.ValidAt(ctx, ns, now, nil)
	if err != nil {
		return nil, err
	}

	result := &RetractResult{}
	for _, n := range nodes {
		sid, ok := n.Properties["source_id"].(string)
		if !ok || sid != sourceExternalID {
			continue
		}
		if err := r.graph.RetractNode(ctx, ns, n.ID, reason, now); err != nil {
			continue // non-fatal per node
		}
		result.NodesRetracted++
		result.NodeIDs = append(result.NodeIDs, n.ID)
	}
	return result, nil
}

// RetractByLabel retracts all currently-valid nodes carrying a specific label.
func (r *BulkRetractor) RetractByLabel(ctx context.Context, ns, label, reason string) (*RetractResult, error) {
	now := time.Now()
	nodes, err := r.graph.ValidAt(ctx, ns, now, []string{label})
	if err != nil {
		return nil, err
	}

	result := &RetractResult{}
	for _, n := range nodes {
		if err := r.graph.RetractNode(ctx, ns, n.ID, reason, now); err != nil {
			continue
		}
		result.NodesRetracted++
		result.NodeIDs = append(result.NodeIDs, n.ID)
	}
	return result, nil
}

// CascadeRetract retracts a node and all nodes that derive from it,
// recursively up to maxDepth. If maxDepth is 0, defaults to 10.
func (r *BulkRetractor) CascadeRetract(ctx context.Context, ns string, nodeID uuid.UUID, reason string, maxDepth int) (*RetractResult, error) {
	if maxDepth <= 0 {
		maxDepth = 10
	}

	now := time.Now()
	result := &RetractResult{}
	visited := make(map[uuid.UUID]bool)

	if err := r.cascadeWalk(ctx, ns, nodeID, reason, now, maxDepth, 0, visited, result); err != nil {
		return result, err
	}
	return result, nil
}

func (r *BulkRetractor) cascadeWalk(ctx context.Context, ns string, nodeID uuid.UUID, reason string, at time.Time, maxDepth, depth int, visited map[uuid.UUID]bool, result *RetractResult) error {
	if depth > maxDepth || visited[nodeID] {
		return nil
	}
	visited[nodeID] = true

	// Retract this node
	if err := r.graph.RetractNode(ctx, ns, nodeID, reason, at); err != nil {
		// May already be retracted — continue cascade
		_ = err
	}
	result.NodesRetracted++
	result.NodeIDs = append(result.NodeIDs, nodeID)
	if depth > result.CascadeDepth {
		result.CascadeDepth = depth
	}

	// Find nodes that derive FROM this node (this node is the source of derivation)
	edges, err := r.graph.EdgesFrom(ctx, ns, nodeID, []string{core.EdgeDerivedFrom})
	if err != nil {
		return nil
	}

	for _, e := range edges {
		if err := r.cascadeWalk(ctx, ns, e.Dst, fmt.Sprintf("cascade from %s: %s", nodeID, reason), at, maxDepth, depth+1, visited, result); err != nil {
			return err
		}
	}
	return nil
}
