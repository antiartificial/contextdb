package retrieval

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store/memory"
)

const testNS = "test-conflicts"

func makeNode(ns string, confidence float64) core.Node {
	now := time.Now()
	return core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Confidence: confidence,
		ValidFrom:  now,
		TxTime:     now,
	}
}

func makeContradiction(ns string, src, dst uuid.UUID) core.Edge {
	return core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       src,
		Dst:       dst,
		Type:      core.EdgeContradicts,
		Weight:    1.0,
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}
}

// TestFindConflictClusters_TwoPairs verifies that two independent contradiction
// pairs are grouped into two separate clusters with correct credibility gaps.
func TestFindConflictClusters_TwoPairs(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	// Cluster 1: A (0.9) contradicts B (0.3) — gap 0.6
	nodeA := makeNode(testNS, 0.9)
	nodeB := makeNode(testNS, 0.3)

	// Cluster 2: C (0.7) contradicts D (0.5) — gap 0.2
	nodeC := makeNode(testNS, 0.7)
	nodeD := makeNode(testNS, 0.5)

	for _, n := range []core.Node{nodeA, nodeB, nodeC, nodeD} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	edgeAB := makeContradiction(testNS, nodeA.ID, nodeB.ID)
	edgeCD := makeContradiction(testNS, nodeC.ID, nodeD.ID)
	for _, e := range []core.Edge{edgeAB, edgeCD} {
		if err := g.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge: %v", err)
		}
	}

	clusters, err := FindConflictClusters(ctx, g, testNS, nil)
	if err != nil {
		t.Fatalf("FindConflictClusters: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	// Clusters are sorted by credibility gap descending.
	// Cluster 0 should be the A-B pair (gap 0.6).
	if clusters[0].CredibilityGap < clusters[1].CredibilityGap {
		t.Errorf("clusters not sorted by credibility gap: [0]=%v [1]=%v",
			clusters[0].CredibilityGap, clusters[1].CredibilityGap)
	}

	const epsilon = 1e-9
	wantGap0 := 0.9 - 0.3
	if diff := clusters[0].CredibilityGap - wantGap0; diff < -epsilon || diff > epsilon {
		t.Errorf("cluster[0].CredibilityGap = %v, want %v", clusters[0].CredibilityGap, wantGap0)
	}

	wantGap1 := 0.7 - 0.5
	if diff := clusters[1].CredibilityGap - wantGap1; diff < -epsilon || diff > epsilon {
		t.Errorf("cluster[1].CredibilityGap = %v, want %v", clusters[1].CredibilityGap, wantGap1)
	}

	// Each cluster should have exactly 2 nodes and 1 edge.
	for i, c := range clusters {
		if len(c.Nodes) != 2 {
			t.Errorf("cluster[%d]: expected 2 nodes, got %d", i, len(c.Nodes))
		}
		if len(c.Edges) != 1 {
			t.Errorf("cluster[%d]: expected 1 edge, got %d", i, len(c.Edges))
		}
	}
}

// TestFindConflictClusters_NoContradictions verifies that a store with nodes
// but no contradiction edges returns nil.
func TestFindConflictClusters_NoContradictions(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	nodeA := makeNode(testNS, 0.8)
	nodeB := makeNode(testNS, 0.6)
	for _, n := range []core.Node{nodeA, nodeB} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}
	// Add a non-contradiction edge to make sure it is ignored.
	supportEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: testNS,
		Src:       nodeA.ID,
		Dst:       nodeB.ID,
		Type:      core.EdgeSupports,
		Weight:    1.0,
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}
	if err := g.UpsertEdge(ctx, supportEdge); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	clusters, err := FindConflictClusters(ctx, g, testNS, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clusters != nil {
		t.Errorf("expected nil clusters, got %v", clusters)
	}
}

// TestFindConflictClusters_ZeroConfidenceTreatedAsHalf verifies that nodes with
// Confidence == 0 are treated as 0.5 when computing the credibility gap.
func TestFindConflictClusters_ZeroConfidenceTreatedAsHalf(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	// nodeE has confidence 0 (neutral), nodeF has 0.9 — gap should be 0.4.
	nodeE := makeNode(testNS, 0.0)
	nodeF := makeNode(testNS, 0.9)
	for _, n := range []core.Node{nodeE, nodeF} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}
	if err := g.UpsertEdge(ctx, makeContradiction(testNS, nodeE.ID, nodeF.ID)); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	clusters, err := FindConflictClusters(ctx, g, testNS, nil)
	if err != nil {
		t.Fatalf("FindConflictClusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}

	const epsilon = 1e-9
	wantGap := 0.9 - 0.5 // 0 treated as 0.5
	if diff := clusters[0].CredibilityGap - wantGap; diff < -epsilon || diff > epsilon {
		t.Errorf("CredibilityGap = %v, want %v", clusters[0].CredibilityGap, wantGap)
	}
}

// TestFindConflictClusters_ExplicitNodeIDs verifies that passing explicit node
// IDs restricts the scan to only those nodes.
func TestFindConflictClusters_ExplicitNodeIDs(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	nodeA := makeNode(testNS, 0.9)
	nodeB := makeNode(testNS, 0.2)
	nodeC := makeNode(testNS, 0.7)
	nodeD := makeNode(testNS, 0.4)

	for _, n := range []core.Node{nodeA, nodeB, nodeC, nodeD} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}
	// Both pairs contradict each other.
	for _, e := range []core.Edge{
		makeContradiction(testNS, nodeA.ID, nodeB.ID),
		makeContradiction(testNS, nodeC.ID, nodeD.ID),
	} {
		if err := g.UpsertEdge(ctx, e); err != nil {
			t.Fatalf("UpsertEdge: %v", err)
		}
	}

	// Only pass A and B — the C-D cluster should not appear.
	clusters, err := FindConflictClusters(ctx, g, testNS, []uuid.UUID{nodeA.ID, nodeB.ID})
	if err != nil {
		t.Fatalf("FindConflictClusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster when scoping to A+B, got %d", len(clusters))
	}
	if len(clusters[0].Nodes) != 2 {
		t.Errorf("expected 2 nodes in cluster, got %d", len(clusters[0].Nodes))
	}
}
