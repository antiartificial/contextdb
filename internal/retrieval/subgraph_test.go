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

const subgraphNS = "test:subgraph"

// buildChain creates nodes A→B→C and returns their IDs in order.
func buildChain(t *testing.T, graph *memstore.GraphStore) [3]uuid.UUID {
	t.Helper()
	ctx := context.Background()

	nodes := [3]core.Node{
		{ID: uuid.New(), Namespace: subgraphNS, Labels: []string{"Claim"},
			Properties: map[string]any{"text": "A"}, Confidence: 0.9, ValidFrom: time.Now()},
		{ID: uuid.New(), Namespace: subgraphNS, Labels: []string{"Claim"},
			Properties: map[string]any{"text": "B"}, Confidence: 0.8, ValidFrom: time.Now()},
		{ID: uuid.New(), Namespace: subgraphNS, Labels: []string{"Claim"},
			Properties: map[string]any{"text": "C"}, Confidence: 0.7, ValidFrom: time.Now()},
	}
	for _, n := range nodes {
		if err := graph.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert node: %v", err)
		}
	}

	edges := []core.Edge{
		{ID: uuid.New(), Namespace: subgraphNS, Src: nodes[0].ID, Dst: nodes[1].ID,
			Type: core.EdgeRelatesTo, Weight: 1.0, ValidFrom: time.Now()},
		{ID: uuid.New(), Namespace: subgraphNS, Src: nodes[1].ID, Dst: nodes[2].ID,
			Type: core.EdgeRelatesTo, Weight: 1.0, ValidFrom: time.Now()},
	}
	for _, e := range edges {
		if err := graph.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("upsert edge: %v", err)
		}
	}

	return [3]uuid.UUID{nodes[0].ID, nodes[1].ID, nodes[2].ID}
}

func TestExtractSubgraph_FullChain(t *testing.T) {
	is := is.New(t)
	graph := memstore.NewGraphStore()
	ids := buildChain(t, graph)

	// BFS processes depths 0..maxDepth-1, so maxDepth=3 reaches A(0), B(1), C(2).
	sg, err := retrieval.ExtractSubgraph(context.Background(), graph, subgraphNS, []uuid.UUID{ids[0]}, 3)
	is.NoErr(err)
	is.True(sg != nil)

	// All 3 nodes must be present
	is.Equal(3, len(sg.Nodes))

	// Both edges must be present
	is.Equal(2, len(sg.Edges))

	// Verify node IDs
	gotNodes := make(map[uuid.UUID]bool, len(sg.Nodes))
	for _, n := range sg.Nodes {
		gotNodes[n.ID] = true
	}
	is.True(gotNodes[ids[0]])
	is.True(gotNodes[ids[1]])
	is.True(gotNodes[ids[2]])

	// Verify edges connect expected endpoints
	type ep struct{ src, dst uuid.UUID }
	gotEdges := make(map[ep]bool, len(sg.Edges))
	for _, e := range sg.Edges {
		gotEdges[ep{e.Src, e.Dst}] = true
	}
	is.True(gotEdges[ep{ids[0], ids[1]}])
	is.True(gotEdges[ep{ids[1], ids[2]}])
}

func TestExtractSubgraph_DepthTwo(t *testing.T) {
	is := is.New(t)
	graph := memstore.NewGraphStore()
	ids := buildChain(t, graph)

	// BFS processes depths 0..maxDepth-1: maxDepth=2 visits A(depth 0) and B(depth 1).
	// C is queued at depth 1 but not processed because depth 2 == maxDepth.
	sg, err := retrieval.ExtractSubgraph(context.Background(), graph, subgraphNS, []uuid.UUID{ids[0]}, 2)
	is.NoErr(err)
	is.True(sg != nil)

	// Only A and B are reachable within maxDepth=2
	is.Equal(2, len(sg.Nodes))
	is.Equal(1, len(sg.Edges))

	gotNodes := make(map[uuid.UUID]bool, len(sg.Nodes))
	for _, n := range sg.Nodes {
		gotNodes[n.ID] = true
	}
	is.True(gotNodes[ids[0]])
	is.True(gotNodes[ids[1]])
	is.True(!gotNodes[ids[2]])

	// The single edge must be A→B
	is.Equal(ids[0], sg.Edges[0].Src)
	is.Equal(ids[1], sg.Edges[0].Dst)
}

func TestExtractSubgraph_DefaultDepth(t *testing.T) {
	is := is.New(t)
	graph := memstore.NewGraphStore()
	ids := buildChain(t, graph)

	// maxDepth=0 should default to 3, reaching all nodes
	sg, err := retrieval.ExtractSubgraph(context.Background(), graph, subgraphNS, []uuid.UUID{ids[0]}, 0)
	is.NoErr(err)
	is.Equal(3, len(sg.Nodes))
	is.Equal(2, len(sg.Edges))
}

func TestExtractSubgraph_EmptyStore(t *testing.T) {
	is := is.New(t)
	graph := memstore.NewGraphStore()

	sg, err := retrieval.ExtractSubgraph(context.Background(), graph, subgraphNS, []uuid.UUID{uuid.New()}, 3)
	is.NoErr(err)
	is.True(sg != nil)
	is.Equal(0, len(sg.Nodes))
	is.Equal(0, len(sg.Edges))
}
