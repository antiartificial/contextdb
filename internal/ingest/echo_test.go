package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

const testNS = "echo-test"

// makeNode creates a minimal Node with a fresh ID in the given namespace.
func makeNode(ns string) core.Node {
	return core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}
}

// makeSupportsEdge creates a "supports" edge from src to dst.
func makeSupportsEdge(ns string, src, dst uuid.UUID) core.Edge {
	return core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       src,
		Dst:       dst,
		Type:      core.EdgeSupports,
		Weight:    1.0,
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}
}

// TestEchoDetector_NoEchoChamber: two sources that support each other only once
// (below the mutual-support threshold) → no alerts.
func TestEchoDetector_NoEchoChamber(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewEchoDetector(graph)

	// Source A has one node, Source B has one node.
	nodeA := makeNode(testNS)
	nodeB := makeNode(testNS)
	is.NoErr(graph.UpsertNode(ctx, nodeA))
	is.NoErr(graph.UpsertNode(ctx, nodeB))

	// A supports B once, B supports A once — below the threshold of 2 each.
	is.NoErr(graph.UpsertEdge(ctx, makeSupportsEdge(testNS, nodeA.ID, nodeB.ID)))
	is.NoErr(graph.UpsertEdge(ctx, makeSupportsEdge(testNS, nodeB.ID, nodeA.ID)))

	sourceNodes := map[string][]core.Node{
		"source-a": {nodeA},
		"source-b": {nodeB},
	}

	alerts, err := detector.Detect(ctx, testNS, sourceNodes)
	is.NoErr(err)
	is.Equal(len(alerts), 0)
}

// TestEchoDetector_MutualSupport: two sources that mutually support each other
// 3+ times in both directions → alert is raised with confidence > 0.3.
func TestEchoDetector_MutualSupport(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewEchoDetector(graph)

	// Give each source three nodes so we can create multiple support edges.
	nodesA := []core.Node{makeNode(testNS), makeNode(testNS), makeNode(testNS)}
	nodesB := []core.Node{makeNode(testNS), makeNode(testNS), makeNode(testNS)}

	for _, n := range append(nodesA, nodesB...) {
		is.NoErr(graph.UpsertNode(ctx, n))
	}

	// A → B: 3 edges (nodeA[0]→nodeB[0], nodeA[1]→nodeB[1], nodeA[2]→nodeB[2])
	for i := 0; i < 3; i++ {
		is.NoErr(graph.UpsertEdge(ctx, makeSupportsEdge(testNS, nodesA[i].ID, nodesB[i].ID)))
	}
	// B → A: 3 edges
	for i := 0; i < 3; i++ {
		is.NoErr(graph.UpsertEdge(ctx, makeSupportsEdge(testNS, nodesB[i].ID, nodesA[i].ID)))
	}

	sourceNodes := map[string][]core.Node{
		"source-a": nodesA,
		"source-b": nodesB,
	}

	alerts, err := detector.Detect(ctx, testNS, sourceNodes)
	is.NoErr(err)
	is.Equal(len(alerts), 1)

	alert := alerts[0]
	is.Equal(len(alert.SourceIDs), 2)
	is.True(alert.Confidence > 0.3)
	is.Equal(alert.Reason, "mutual support pattern detected")
}

// TestEchoDetector_SingleSource: only one source provided → no alerts
// (need at least 2 sources to form a cluster).
func TestEchoDetector_SingleSource(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewEchoDetector(graph)

	node := makeNode(testNS)
	is.NoErr(graph.UpsertNode(ctx, node))

	sourceNodes := map[string][]core.Node{
		"source-only": {node},
	}

	alerts, err := detector.Detect(ctx, testNS, sourceNodes)
	is.NoErr(err)
	is.Equal(len(alerts), 0)
}
