package retrieval

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store/memory"
)

const diffNS = "test-belief-diff"

func makeDiffNode(ns string, confidence float64) core.Node {
	now := time.Now()
	return core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Confidence: confidence,
		ValidFrom:  now,
		TxTime:     now,
	}
}

func makeDiffContradiction(ns string, src, dst uuid.UUID, weight float64) core.Edge {
	return core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       src,
		Dst:       dst,
		Type:      core.EdgeContradicts,
		Weight:    weight,
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}
}

// TestComputeBeliefDiff_OneConflict creates two nodes with a contradiction edge
// and verifies the diff contains exactly one conflict with the correct sides.
func TestComputeBeliefDiff_OneConflict(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	nodeA := makeDiffNode(diffNS, 0.9)
	nodeB := makeDiffNode(diffNS, 0.3)

	for _, n := range []core.Node{nodeA, nodeB} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	edge := makeDiffContradiction(diffNS, nodeA.ID, nodeB.ID, 0.8)
	if err := g.UpsertEdge(ctx, edge); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	diff, err := ComputeBeliefDiff(ctx, g, diffNS, nil)
	if err != nil {
		t.Fatalf("ComputeBeliefDiff: %v", err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if diff.TotalConflicts != 1 {
		t.Fatalf("expected 1 conflict, got %d", diff.TotalConflicts)
	}

	conflict := diff.Conflicts[0]

	// Verify the two sides correspond to nodeA and nodeB (order follows edge Src/Dst).
	if conflict.ClaimA.Node.ID != nodeA.ID {
		t.Errorf("ClaimA.Node.ID = %v, want %v", conflict.ClaimA.Node.ID, nodeA.ID)
	}
	if conflict.ClaimB.Node.ID != nodeB.ID {
		t.Errorf("ClaimB.Node.ID = %v, want %v", conflict.ClaimB.Node.ID, nodeB.ID)
	}

	// Verify confidence values are propagated correctly.
	if conflict.ClaimA.Confidence != 0.9 {
		t.Errorf("ClaimA.Confidence = %v, want 0.9", conflict.ClaimA.Confidence)
	}
	if conflict.ClaimB.Confidence != 0.3 {
		t.Errorf("ClaimB.Confidence = %v, want 0.3", conflict.ClaimB.Confidence)
	}

	// Verify contradiction weight is passed through.
	if conflict.ContradictionWeight != 0.8 {
		t.Errorf("ContradictionWeight = %v, want 0.8", conflict.ContradictionWeight)
	}

	// Verify namespace and metadata.
	if diff.Namespace != diffNS {
		t.Errorf("Namespace = %q, want %q", diff.Namespace, diffNS)
	}
	if diff.ComputedAt.IsZero() {
		t.Error("ComputedAt should not be zero")
	}
}

// TestComputeBeliefDiff_NoContradictions verifies that a store with nodes but
// no contradiction edges returns a diff with zero conflicts.
func TestComputeBeliefDiff_NoContradictions(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	nodeA := makeDiffNode(diffNS, 0.8)
	nodeB := makeDiffNode(diffNS, 0.6)
	for _, n := range []core.Node{nodeA, nodeB} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	// Add a supports edge — should not trigger any conflict.
	supportEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: diffNS,
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

	diff, err := ComputeBeliefDiff(ctx, g, diffNS, nil)
	if err != nil {
		t.Fatalf("ComputeBeliefDiff: %v", err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff (even with zero conflicts)")
	}
	if diff.TotalConflicts != 0 {
		t.Errorf("expected 0 conflicts, got %d", diff.TotalConflicts)
	}
	if len(diff.Conflicts) != 0 {
		t.Errorf("expected empty Conflicts slice, got %d entries", len(diff.Conflicts))
	}
	if diff.AvgCredibilityGap != 0 {
		t.Errorf("AvgCredibilityGap = %v, want 0", diff.AvgCredibilityGap)
	}
}

// TestComputeBeliefDiff_CredibilityGap verifies the gap is always the absolute
// difference between the two sides' confidence values.
func TestComputeBeliefDiff_CredibilityGap(t *testing.T) {
	cases := []struct {
		name    string
		confA   float64
		confB   float64
		wantGap float64
	}{
		{"high-vs-low", 0.9, 0.2, 0.7},
		{"low-vs-high", 0.2, 0.9, 0.7},  // order should not matter
		{"equal", 0.5, 0.5, 0.0},
		{"zero-treated-as-half", 0.0, 0.9, 0.4}, // 0 → 0.5, gap = 0.9 - 0.5
	}

	const epsilon = 1e-9

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			g := memory.NewGraphStore()

			nodeA := makeDiffNode(diffNS, tc.confA)
			nodeB := makeDiffNode(diffNS, tc.confB)
			for _, n := range []core.Node{nodeA, nodeB} {
				if err := g.UpsertNode(ctx, n); err != nil {
					t.Fatalf("UpsertNode: %v", err)
				}
			}
			if err := g.UpsertEdge(ctx, makeDiffContradiction(diffNS, nodeA.ID, nodeB.ID, 1.0)); err != nil {
				t.Fatalf("UpsertEdge: %v", err)
			}

			diff, err := ComputeBeliefDiff(ctx, g, diffNS, nil)
			if err != nil {
				t.Fatalf("ComputeBeliefDiff: %v", err)
			}
			if diff.TotalConflicts != 1 {
				t.Fatalf("expected 1 conflict, got %d", diff.TotalConflicts)
			}

			got := diff.Conflicts[0].CredibilityGap
			if d := got - tc.wantGap; d < -epsilon || d > epsilon {
				t.Errorf("CredibilityGap = %v, want %v", got, tc.wantGap)
			}

			// AvgCredibilityGap should equal the single conflict's gap.
			if d := diff.AvgCredibilityGap - tc.wantGap; d < -epsilon || d > epsilon {
				t.Errorf("AvgCredibilityGap = %v, want %v", diff.AvgCredibilityGap, tc.wantGap)
			}
		})
	}
}

// TestComputeBeliefDiff_SupporterCount verifies that supporter edges are counted
// on each conflict side.
func TestComputeBeliefDiff_SupporterCount(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	nodeA := makeDiffNode(diffNS, 0.8)
	nodeB := makeDiffNode(diffNS, 0.4)
	supporter := makeDiffNode(diffNS, 0.9)

	for _, n := range []core.Node{nodeA, nodeB, supporter} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	// supporter → supports → nodeA
	supportEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: diffNS,
		Src:       supporter.ID,
		Dst:       nodeA.ID,
		Type:      core.EdgeSupports,
		Weight:    1.0,
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}
	if err := g.UpsertEdge(ctx, supportEdge); err != nil {
		t.Fatalf("UpsertEdge (support): %v", err)
	}

	contradictionEdge := makeDiffContradiction(diffNS, nodeA.ID, nodeB.ID, 1.0)
	if err := g.UpsertEdge(ctx, contradictionEdge); err != nil {
		t.Fatalf("UpsertEdge (contradiction): %v", err)
	}

	diff, err := ComputeBeliefDiff(ctx, g, diffNS, nil)
	if err != nil {
		t.Fatalf("ComputeBeliefDiff: %v", err)
	}
	if diff.TotalConflicts != 1 {
		t.Fatalf("expected 1 conflict, got %d", diff.TotalConflicts)
	}

	conflict := diff.Conflicts[0]
	if conflict.ClaimA.SupporterCount != 1 {
		t.Errorf("ClaimA.SupporterCount = %d, want 1", conflict.ClaimA.SupporterCount)
	}
	if conflict.ClaimB.SupporterCount != 0 {
		t.Errorf("ClaimB.SupporterCount = %d, want 0", conflict.ClaimB.SupporterCount)
	}
}

// TestComputeBeliefDiff_SourceID verifies that source_id is extracted from
// node properties and surfaced on each ConflictSide.
func TestComputeBeliefDiff_SourceID(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	now := time.Now()
	nodeA := core.Node{
		ID:         uuid.New(),
		Namespace:  diffNS,
		Confidence: 0.7,
		ValidFrom:  now,
		TxTime:     now,
		Properties: map[string]any{"source_id": "src-alpha"},
	}
	nodeB := core.Node{
		ID:         uuid.New(),
		Namespace:  diffNS,
		Confidence: 0.4,
		ValidFrom:  now,
		TxTime:     now,
		Properties: map[string]any{"source_id": "src-beta"},
	}

	for _, n := range []core.Node{nodeA, nodeB} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}
	if err := g.UpsertEdge(ctx, makeDiffContradiction(diffNS, nodeA.ID, nodeB.ID, 1.0)); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	diff, err := ComputeBeliefDiff(ctx, g, diffNS, nil)
	if err != nil {
		t.Fatalf("ComputeBeliefDiff: %v", err)
	}
	if diff.TotalConflicts != 1 {
		t.Fatalf("expected 1 conflict, got %d", diff.TotalConflicts)
	}

	conflict := diff.Conflicts[0]
	if conflict.ClaimA.SourceID != "src-alpha" {
		t.Errorf("ClaimA.SourceID = %q, want %q", conflict.ClaimA.SourceID, "src-alpha")
	}
	if conflict.ClaimB.SourceID != "src-beta" {
		t.Errorf("ClaimB.SourceID = %q, want %q", conflict.ClaimB.SourceID, "src-beta")
	}
}
