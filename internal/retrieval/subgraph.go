package retrieval

import (
	"context"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// Subgraph is a connected component of nodes and edges.
type Subgraph struct {
	Nodes []core.Node
	Edges []core.Edge
}

// ExtractSubgraph returns the connected subgraph around the given seed nodes
// within maxDepth hops. Unlike Walk (which returns a flat node list), this
// preserves the edge structure.
func ExtractSubgraph(ctx context.Context, graph store.GraphStore, ns string, seedIDs []uuid.UUID, maxDepth int) (*Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	// First, get all nodes via Walk
	nodes, err := graph.Walk(ctx, store.WalkQuery{
		Namespace: ns,
		SeedIDs:   seedIDs,
		MaxDepth:  maxDepth,
		Strategy:  store.StrategyBFS,
	})
	if err != nil {
		return nil, err
	}

	// Build node ID set
	nodeSet := make(map[uuid.UUID]bool, len(nodes))
	for _, n := range nodes {
		nodeSet[n.ID] = true
	}

	// Collect edges between nodes in the subgraph
	var edges []core.Edge
	edgeSeen := make(map[uuid.UUID]bool)

	for _, n := range nodes {
		nodeEdges, err := graph.GetEdges(ctx, ns, n.ID)
		if err != nil {
			continue
		}
		for _, e := range nodeEdges {
			if edgeSeen[e.ID] {
				continue
			}
			// Only include edges where both endpoints are in the subgraph
			if nodeSet[e.Src] && nodeSet[e.Dst] {
				edges = append(edges, e)
				edgeSeen[e.ID] = true
			}
		}
	}

	return &Subgraph{Nodes: nodes, Edges: edges}, nil
}
