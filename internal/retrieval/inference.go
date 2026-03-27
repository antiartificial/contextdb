package retrieval

import (
	"context"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// InferenceChain represents a chain of supporting evidence for a claim.
type InferenceChain struct {
	// Target is the node being evaluated.
	Target core.Node
	// Links are the supporting nodes in order from Target backward to root.
	Links []InferenceLink
	// CompoundConfidence is the product of confidence * edge weight along the chain.
	CompoundConfidence float64
}

// InferenceLink is one hop in an inference chain.
type InferenceLink struct {
	Node       core.Node
	Edge       core.Edge
	Confidence float64 // this node's contribution to the chain
}

// TraceInferenceChain walks "supports" edges backward from the given nodeID,
// building the evidence chain and computing compound confidence.
// MaxDepth caps traversal (default 20). Returns the chain or an empty chain
// if the node has no supporting evidence.
func TraceInferenceChain(ctx context.Context, graph store.GraphStore, ns string, nodeID uuid.UUID, maxDepth int) (*InferenceChain, error) {
	if maxDepth <= 0 {
		maxDepth = 20
	}

	target, err := graph.GetNode(ctx, ns, nodeID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, nil
	}

	chain := &InferenceChain{
		Target:             *target,
		CompoundConfidence: target.Confidence,
	}
	if chain.CompoundConfidence == 0 {
		chain.CompoundConfidence = 0.5 // neutral default
	}

	visited := map[uuid.UUID]bool{nodeID: true}
	currentID := nodeID
	compound := chain.CompoundConfidence

	for depth := 0; depth < maxDepth; depth++ {
		edges, err := graph.EdgesTo(ctx, ns, currentID, []string{core.EdgeSupports})
		if err != nil || len(edges) == 0 {
			break
		}

		// Follow the strongest supporting edge
		var bestEdge core.Edge
		var bestWeight float64
		for _, e := range edges {
			if !visited[e.Src] && e.Weight > bestWeight {
				bestEdge = e
				bestWeight = e.Weight
			}
		}
		if bestWeight == 0 {
			break
		}

		supporter, err := graph.GetNode(ctx, ns, bestEdge.Src)
		if err != nil || supporter == nil {
			break
		}

		visited[bestEdge.Src] = true

		conf := supporter.Confidence
		if conf == 0 {
			conf = 0.5
		}

		// Compound: multiply edge weight and supporter confidence
		compound *= bestEdge.Weight * conf

		chain.Links = append(chain.Links, InferenceLink{
			Node:       *supporter,
			Edge:       bestEdge,
			Confidence: conf,
		})

		currentID = bestEdge.Src
	}

	chain.CompoundConfidence = compound
	return chain, nil
}
