package retrieval

import (
	"context"
	"sort"

	"github.com/antiartificial/contextdb/internal/core"
)

// ContrastivePair pairs a retrieved node with its strongest contradiction.
type ContrastivePair struct {
	Node         core.ScoredNode
	Contradictor *core.ScoredNode // nil if no contradiction exists
	EdgeWeight   float64          // strength of the contradiction [0, 1]
}

// RetrieveContrastive runs a standard retrieval, then for each result
// finds its strongest contradictor via "contradicts" edges.
func (e *Engine) RetrieveContrastive(ctx context.Context, q Query) ([]ContrastivePair, error) {
	results, err := e.Retrieve(ctx, q)
	if err != nil {
		return nil, err
	}
	if e.Graph == nil {
		// No graph store -- return results without contradictors
		pairs := make([]ContrastivePair, len(results))
		for i, r := range results {
			pairs[i] = ContrastivePair{Node: r}
		}
		return pairs, nil
	}

	pairs := make([]ContrastivePair, len(results))
	for i, r := range results {
		pair := ContrastivePair{Node: r}

		edges, err := e.Graph.EdgesFrom(ctx, q.Namespace, r.Node.ID, []string{core.EdgeContradicts})
		if err != nil {
			// Non-fatal -- just skip contradictor lookup
			pairs[i] = pair
			continue
		}

		// Also check edges pointing TO this node (bidirectional contradictions)
		edgesTo, err := e.Graph.EdgesTo(ctx, q.Namespace, r.Node.ID, []string{core.EdgeContradicts})
		if err == nil {
			edges = append(edges, edgesTo...)
		}

		if len(edges) == 0 {
			pairs[i] = pair
			continue
		}

		// Find the strongest contradiction by edge weight
		sort.Slice(edges, func(a, b int) bool {
			return edges[a].Weight > edges[b].Weight
		})

		bestEdge := edges[0]
		// The contradictor is the other end of the edge
		contradictorID := bestEdge.Dst
		if contradictorID == r.Node.ID {
			contradictorID = bestEdge.Src
		}

		cNode, err := e.Graph.GetNode(ctx, q.Namespace, contradictorID)
		if err != nil || cNode == nil {
			pairs[i] = pair
			continue
		}

		// Score the contradictor with the same params
		scored := core.ScoreNode(*cNode, 0, 1.0, q.ScoreParams)
		scored.RetrievalSource = "contrastive"
		pair.Contradictor = &scored
		pair.EdgeWeight = bestEdge.Weight

		pairs[i] = pair
	}

	return pairs, nil
}
