package retrieval

import (
	"context"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// ComputeProvenanceDepth walks derives_from edges backward from nodeID and
// returns the chain length. For example, "Alice told me Bob said X" yields
// depth 2 from the leaf node. maxDepth caps traversal to prevent unbounded
// graph walks; if maxDepth <= 0 it defaults to 10.
func ComputeProvenanceDepth(ctx context.Context, graph store.GraphStore, ns string, nodeID uuid.UUID, maxDepth int) int {
	if graph == nil {
		return 0
	}
	if maxDepth <= 0 {
		maxDepth = 10
	}

	currentID := nodeID
	for depth := 0; depth < maxDepth; depth++ {
		edges, err := graph.EdgesTo(ctx, ns, currentID, []string{core.EdgeDerivedFrom})
		if err != nil || len(edges) == 0 {
			return depth
		}
		// Follow the first derives_from edge (primary provenance chain).
		currentID = edges[0].Src
	}
	return maxDepth
}
