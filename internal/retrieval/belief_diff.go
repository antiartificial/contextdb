package retrieval

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// BeliefDiff represents a structured disagreement between two perspectives
// on a set of claims. It's "git diff for beliefs."
type BeliefDiff struct {
	Namespace  string
	ComputedAt time.Time

	// Conflicts are groups of contradicting nodes.
	Conflicts []BeliefConflict

	// Summary statistics
	TotalConflicts    int
	AvgCredibilityGap float64
}

// BeliefConflict is a single disagreement between claims.
type BeliefConflict struct {
	// ClaimA and ClaimB are the two contradicting nodes.
	ClaimA ConflictSide
	ClaimB ConflictSide

	// ContradictionWeight is the strength of the contradiction [0, 1].
	ContradictionWeight float64

	// CredibilityGap is |ClaimA.Confidence - ClaimB.Confidence|.
	CredibilityGap float64
}

// ConflictSide is one side of a belief conflict.
type ConflictSide struct {
	Node           core.Node
	Confidence     float64
	SourceID       string
	EvidenceChain  *InferenceChain // may be nil if no supports chain
	SupporterCount int
}

// ComputeBeliefDiff analyzes all contradictions in a namespace and returns
// a structured diff. Optionally scope to specific nodeIDs (nil = all valid nodes).
func ComputeBeliefDiff(ctx context.Context, graph store.GraphStore, ns string, nodeIDs []uuid.UUID) (*BeliefDiff, error) {
	clusters, err := FindConflictClusters(ctx, graph, ns, nodeIDs)
	if err != nil {
		return nil, err
	}

	diff := &BeliefDiff{
		Namespace:  ns,
		ComputedAt: time.Now(),
	}

	var totalGap float64

	for _, cluster := range clusters {
		// For each pair of contradicting nodes in the cluster
		for _, edge := range cluster.Edges {
			var nodeA, nodeB *core.Node
			for i := range cluster.Nodes {
				if cluster.Nodes[i].ID == edge.Src {
					nodeA = &cluster.Nodes[i]
				}
				if cluster.Nodes[i].ID == edge.Dst {
					nodeB = &cluster.Nodes[i]
				}
			}
			if nodeA == nil || nodeB == nil {
				continue
			}

			sideA := buildConflictSide(ctx, graph, ns, *nodeA)
			sideB := buildConflictSide(ctx, graph, ns, *nodeB)

			confA := sideA.Confidence
			confB := sideB.Confidence
			gap := confA - confB
			if gap < 0 {
				gap = -gap
			}

			conflict := BeliefConflict{
				ClaimA:              sideA,
				ClaimB:              sideB,
				ContradictionWeight: edge.Weight,
				CredibilityGap:      gap,
			}

			diff.Conflicts = append(diff.Conflicts, conflict)
			totalGap += gap
		}
	}

	diff.TotalConflicts = len(diff.Conflicts)
	if diff.TotalConflicts > 0 {
		diff.AvgCredibilityGap = totalGap / float64(diff.TotalConflicts)
	}

	return diff, nil
}

func buildConflictSide(ctx context.Context, graph store.GraphStore, ns string, node core.Node) ConflictSide {
	conf := node.Confidence
	if conf == 0 {
		conf = 0.5
	}

	side := ConflictSide{
		Node:       node,
		Confidence: conf,
	}

	if sid, ok := node.Properties["source_id"].(string); ok {
		side.SourceID = sid
	}

	// Try to trace the evidence chain
	chain, err := TraceInferenceChain(ctx, graph, ns, node.ID, 5)
	if err == nil && chain != nil && len(chain.Links) > 0 {
		side.EvidenceChain = chain
	}

	// Count supporters
	edges, err := graph.EdgesTo(ctx, ns, node.ID, []string{core.EdgeSupports})
	if err == nil {
		side.SupporterCount = len(edges)
	}

	return side
}
