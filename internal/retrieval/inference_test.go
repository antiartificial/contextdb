package retrieval_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

const inferenceNS = "test:inference"

func TestTraceInferenceChain_NoSupports(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	target := core.Node{
		ID:         uuid.New(),
		Namespace:  inferenceNS,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "unsupported claim"},
		Confidence: 0.7,
		ValidFrom:  time.Now(),
	}
	is.NoErr(graph.UpsertNode(ctx, target))

	chain, err := retrieval.TraceInferenceChain(ctx, graph, inferenceNS, target.ID, 0)
	is.NoErr(err)
	is.True(chain != nil)
	is.Equal(chain.Target.ID, target.ID)
	is.Equal(len(chain.Links), 0)
	// CompoundConfidence should equal target's confidence when there are no supports edges
	is.Equal(chain.CompoundConfidence, target.Confidence)
}

func TestTraceInferenceChain_TwoHopChain(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	// Build chain: node2 --supports(w=0.9)--> node1 --supports(w=0.8)--> target
	target := core.Node{
		ID:         uuid.New(),
		Namespace:  inferenceNS,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "the target claim"},
		Confidence: 0.8,
		ValidFrom:  time.Now(),
	}
	node1 := core.Node{
		ID:         uuid.New(),
		Namespace:  inferenceNS,
		Labels:     []string{"Evidence"},
		Properties: map[string]any{"text": "first supporting evidence"},
		Confidence: 0.9,
		ValidFrom:  time.Now(),
	}
	node2 := core.Node{
		ID:         uuid.New(),
		Namespace:  inferenceNS,
		Labels:     []string{"Evidence"},
		Properties: map[string]any{"text": "second supporting evidence"},
		Confidence: 0.75,
		ValidFrom:  time.Now(),
	}

	is.NoErr(graph.UpsertNode(ctx, target))
	is.NoErr(graph.UpsertNode(ctx, node1))
	is.NoErr(graph.UpsertNode(ctx, node2))

	edge1Weight := 0.8
	edge2Weight := 0.9

	// node1 supports target
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: inferenceNS,
		Src:       node1.ID,
		Dst:       target.ID,
		Type:      core.EdgeSupports,
		Weight:    edge1Weight,
		ValidFrom: time.Now(),
	}))

	// node2 supports node1
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: inferenceNS,
		Src:       node2.ID,
		Dst:       node1.ID,
		Type:      core.EdgeSupports,
		Weight:    edge2Weight,
		ValidFrom: time.Now(),
	}))

	chain, err := retrieval.TraceInferenceChain(ctx, graph, inferenceNS, target.ID, 20)
	is.NoErr(err)
	is.True(chain != nil)
	is.Equal(chain.Target.ID, target.ID)
	is.Equal(len(chain.Links), 2)

	// Verify the chain order: first link is node1 (directly supports target),
	// second link is node2 (supports node1).
	is.Equal(chain.Links[0].Node.ID, node1.ID)
	is.Equal(chain.Links[1].Node.ID, node2.ID)

	// Compound = target.Confidence * edge1.Weight * node1.Confidence * edge2.Weight * node2.Confidence
	expected := target.Confidence * edge1Weight * node1.Confidence * edge2Weight * node2.Confidence
	// Use a tolerance for floating-point comparison
	diff := chain.CompoundConfidence - expected
	if diff < 0 {
		diff = -diff
	}
	is.True(diff < 1e-9)
}

func TestTraceInferenceChain_NodeNotFound(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	chain, err := retrieval.TraceInferenceChain(ctx, graph, inferenceNS, uuid.New(), 20)
	is.NoErr(err)
	is.True(chain == nil)
}

func TestTraceInferenceChain_MaxDepthRespected(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	// Build a 5-node chain but cap at depth 2 — should only follow 2 hops.
	nodes := make([]core.Node, 5)
	for i := range nodes {
		nodes[i] = core.Node{
			ID:         uuid.New(),
			Namespace:  inferenceNS,
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "node"},
			Confidence: 0.8,
			ValidFrom:  time.Now(),
		}
		is.NoErr(graph.UpsertNode(ctx, nodes[i]))
	}

	// Wire: nodes[i+1] supports nodes[i]
	for i := 0; i < 4; i++ {
		is.NoErr(graph.UpsertEdge(ctx, core.Edge{
			ID:        uuid.New(),
			Namespace: inferenceNS,
			Src:       nodes[i+1].ID,
			Dst:       nodes[i].ID,
			Type:      core.EdgeSupports,
			Weight:    0.9,
			ValidFrom: time.Now(),
		}))
	}

	chain, err := retrieval.TraceInferenceChain(ctx, graph, inferenceNS, nodes[0].ID, 2)
	is.NoErr(err)
	is.True(chain != nil)
	is.Equal(len(chain.Links), 2)
}
