package namespace

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store/memory"
)

func TestComputeHealth_EmptyNamespace(t *testing.T) {
	graph := memory.NewGraphStore()
	ctx := context.Background()

	h, err := ComputeHealth(ctx, graph, "empty-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Overall != 1.0 {
		t.Errorf("expected Overall=1.0 for empty namespace, got %f", h.Overall)
	}
	if h.NodeCount != 0 {
		t.Errorf("expected NodeCount=0, got %d", h.NodeCount)
	}
}

func TestComputeHealth_HealthyNodes(t *testing.T) {
	graph := memory.NewGraphStore()
	ctx := context.Background()
	ns := "healthy-ns"

	// Insert nodes with high confidence and varied sources.
	nodes := []core.Node{
		{
			ID:         uuid.New(),
			Namespace:  ns,
			Confidence: 0.9,
			Properties: map[string]any{"source_id": "src-a"},
			ValidFrom:  time.Now().Add(-time.Hour),
		},
		{
			ID:         uuid.New(),
			Namespace:  ns,
			Confidence: 0.85,
			Properties: map[string]any{"source_id": "src-b"},
			ValidFrom:  time.Now().Add(-time.Hour),
		},
		{
			ID:         uuid.New(),
			Namespace:  ns,
			Confidence: 0.95,
			Properties: map[string]any{"source_id": "src-c"},
			ValidFrom:  time.Now().Add(-time.Hour),
		},
	}
	for _, n := range nodes {
		if err := graph.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	h, err := ComputeHealth(ctx, graph, ns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.NodeCount != 3 {
		t.Errorf("expected NodeCount=3, got %d", h.NodeCount)
	}
	// Avg confidence ~0.9, no conflicts, no staleness, source diversity = 3/3 = 1.0
	// Overall = 0.35*0.9 + 0.25*1.0 + 0.20*1.0 + 0.20*1.0 = 0.315 + 0.25 + 0.20 + 0.20 = 0.965
	if h.Overall < 0.90 {
		t.Errorf("expected high Overall score (>=0.90) for healthy nodes, got %f", h.Overall)
	}
	if h.UniqueSourceCount != 3 {
		t.Errorf("expected UniqueSourceCount=3, got %d", h.UniqueSourceCount)
	}
	if h.SourceDiversity != 1.0 {
		t.Errorf("expected SourceDiversity=1.0, got %f", h.SourceDiversity)
	}
	if h.ConflictCount != 0 {
		t.Errorf("expected ConflictCount=0, got %d", h.ConflictCount)
	}
	if h.ConflictRatio != 0 {
		t.Errorf("expected ConflictRatio=0, got %f", h.ConflictRatio)
	}
}

func TestComputeHealth_WithConflicts(t *testing.T) {
	graph := memory.NewGraphStore()
	ctx := context.Background()
	ns := "conflict-ns"

	// Insert two nodes that contradict each other.
	nodeA := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Confidence: 0.7,
		Properties: map[string]any{"source_id": "src-x"},
		ValidFrom:  time.Now().Add(-time.Hour),
	}
	nodeB := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Confidence: 0.7,
		Properties: map[string]any{"source_id": "src-y"},
		ValidFrom:  time.Now().Add(-time.Hour),
	}
	// A third node with no conflicts.
	nodeC := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Confidence: 0.7,
		Properties: map[string]any{"source_id": "src-z"},
		ValidFrom:  time.Now().Add(-time.Hour),
	}

	for _, n := range []core.Node{nodeA, nodeB, nodeC} {
		if err := graph.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	// Create a contradicts edge from A → B (A is in conflict).
	edge := core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       nodeA.ID,
		Dst:       nodeB.ID,
		Type:      core.EdgeContradicts,
		Weight:    1.0,
		ValidFrom: time.Now().Add(-time.Hour),
	}
	if err := graph.UpsertEdge(ctx, edge); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	withoutConflict, err := ComputeHealth(ctx, graph, "conflict-ns-empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty namespace returns 1.0.
	if withoutConflict.Overall != 1.0 {
		t.Errorf("expected Overall=1.0 for empty namespace, got %f", withoutConflict.Overall)
	}

	h, err := ComputeHealth(ctx, graph, ns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.NodeCount != 3 {
		t.Errorf("expected NodeCount=3, got %d", h.NodeCount)
	}
	// One node (A) has a contradicts edge → ConflictCount=1, ConflictRatio=1/3≈0.333
	if h.ConflictCount != 1 {
		t.Errorf("expected ConflictCount=1, got %d", h.ConflictCount)
	}
	expectedRatio := 1.0 / 3.0
	if h.ConflictRatio < expectedRatio-0.01 || h.ConflictRatio > expectedRatio+0.01 {
		t.Errorf("expected ConflictRatio≈%.3f, got %f", expectedRatio, h.ConflictRatio)
	}

	// Score with conflict should be lower than a fully healthy namespace.
	// Avg conf=0.7, staleness=0, conflict ratio=1/3, source diversity=1.0
	// Overall = 0.35*0.7 + 0.25*1.0 + 0.20*(1-1/3) + 0.20*1.0
	//         = 0.245 + 0.25 + 0.1333 + 0.20 ≈ 0.828
	if h.Overall >= 0.90 {
		t.Errorf("expected Overall < 0.90 with conflicts, got %f", h.Overall)
	}
	if h.Overall <= 0 {
		t.Errorf("expected positive Overall, got %f", h.Overall)
	}
}

func TestComputeHealth_LowConfidence(t *testing.T) {
	graph := memory.NewGraphStore()
	ctx := context.Background()
	ns := "low-conf-ns"

	// Nodes with zero confidence (treated as 0.5) and single source.
	for i := 0; i < 4; i++ {
		n := core.Node{
			ID:         uuid.New(),
			Namespace:  ns,
			Confidence: 0, // treated as 0.5
			ValidFrom:  time.Now().Add(-time.Hour),
		}
		if err := graph.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	h, err := ComputeHealth(ctx, graph, ns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// avg confidence 0.5, no source diversity (0 unique sources / 4 nodes = 0)
	// Overall = 0.35*0.5 + 0.25*1.0 + 0.20*1.0 + 0.20*0 = 0.175 + 0.25 + 0.20 + 0 = 0.625
	if h.AvgConfidence != 0.5 {
		t.Errorf("expected AvgConfidence=0.5 for zero-confidence nodes, got %f", h.AvgConfidence)
	}
	if h.UniqueSourceCount != 0 {
		t.Errorf("expected UniqueSourceCount=0, got %d", h.UniqueSourceCount)
	}
	if h.SourceDiversity != 0 {
		t.Errorf("expected SourceDiversity=0, got %f", h.SourceDiversity)
	}
	// Score should be moderate — below healthy but not zero.
	if h.Overall >= 0.90 {
		t.Errorf("expected Overall < 0.90 for low-confidence nodes, got %f", h.Overall)
	}
	if h.Overall <= 0 {
		t.Errorf("expected positive Overall, got %f", h.Overall)
	}
}

func TestComputeHealth_OverallBounds(t *testing.T) {
	graph := memory.NewGraphStore()
	ctx := context.Background()
	ns := "bounds-ns"

	// Single node, maximum confidence, known source.
	n := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Confidence: 1.0,
		Properties: map[string]any{"source_id": "only-source"},
		ValidFrom:  time.Now().Add(-time.Hour),
	}
	if err := graph.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	h, err := ComputeHealth(ctx, graph, ns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.Overall < 0 || h.Overall > 1 {
		t.Errorf("Overall out of [0,1] bounds: %f", h.Overall)
	}
}
