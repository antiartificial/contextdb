package retrieval

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store/memory"
)

const alNS = "test-active-learning"

// makeALNode creates a node with the given confidence and ValidFrom time.
func makeALNode(ns string, confidence float64, validFrom time.Time, props map[string]any) core.Node {
	if props == nil {
		props = map[string]any{}
	}
	return core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Confidence: confidence,
		ValidFrom:  validFrom,
		TxTime:     validFrom,
		Properties: props,
	}
}

// makeALNodeWithExpiry creates a node that expires at the given time.
func makeALNodeWithExpiry(ns string, confidence float64, validFrom time.Time, expiry time.Time, props map[string]any) core.Node {
	n := makeALNode(ns, confidence, validFrom, props)
	n.ValidUntil = &expiry
	return n
}

// makeContradictEdge creates a contradicts edge between two nodes.
func makeContradictEdge(ns string, src, dst uuid.UUID) core.Edge {
	now := time.Now()
	return core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       src,
		Dst:       dst,
		Type:      core.EdgeContradicts,
		Weight:    1.0,
		ValidFrom: now,
		TxTime:    now,
	}
}

// TestActiveLearner_NoNodes ensures Suggest returns nil when the namespace is empty.
func TestActiveLearner_NoNodes(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()
	learner := NewActiveLearner(g)

	suggestions, err := learner.Suggest(ctx, alNS, 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if suggestions != nil {
		t.Errorf("expected nil suggestions for empty namespace, got %v", suggestions)
	}
}

// TestActiveLearner_LowConfidence verifies that a node with confidence < 0.4
// produces an AcquireLowConfidence suggestion.
func TestActiveLearner_LowConfidence(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	now := time.Now()
	node := makeALNode(alNS, 0.2, now, map[string]any{"text": "The sky is made of cheese"})
	if err := g.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	learner := NewActiveLearner(g)
	suggestions, err := learner.Suggest(ctx, alNS, 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	found := false
	for _, s := range suggestions {
		if s.Type == AcquireLowConfidence {
			found = true
			if s.Priority != 1.0-0.2 {
				t.Errorf("Priority = %v, want %v", s.Priority, 1.0-0.2)
			}
			if len(s.RelatedNodeIDs) != 1 || s.RelatedNodeIDs[0] != node.ID {
				t.Errorf("RelatedNodeIDs = %v, want [%v]", s.RelatedNodeIDs, node.ID)
			}
			if s.Namespace != alNS {
				t.Errorf("Namespace = %q, want %q", s.Namespace, alNS)
			}
		}
	}
	if !found {
		t.Error("expected at least one AcquireLowConfidence suggestion")
	}
}

// TestActiveLearner_RefreshStale verifies that a node expiring within 7 days
// produces an AcquireRefreshStale suggestion.
func TestActiveLearner_RefreshStale(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	now := time.Now()
	expiry := now.Add(48 * time.Hour) // expires in 2 days — within 7-day window
	node := makeALNodeWithExpiry(alNS, 0.8, now, expiry, map[string]any{"text": "Team meeting is on Thursday"})
	if err := g.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	learner := NewActiveLearner(g)
	suggestions, err := learner.Suggest(ctx, alNS, 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	found := false
	for _, s := range suggestions {
		if s.Type == AcquireRefreshStale {
			found = true
			// Priority should be between 0 and 1 (closer to 1 for imminent expiry)
			if s.Priority <= 0 || s.Priority > 1 {
				t.Errorf("Priority = %v, expected (0, 1]", s.Priority)
			}
			if len(s.RelatedNodeIDs) != 1 || s.RelatedNodeIDs[0] != node.ID {
				t.Errorf("RelatedNodeIDs = %v, want [%v]", s.RelatedNodeIDs, node.ID)
			}
		}
	}
	if !found {
		t.Error("expected at least one AcquireRefreshStale suggestion")
	}
}

// TestActiveLearner_VerifyClaim verifies that an old, high-confidence node
// with a contradiction edge produces an AcquireVerifyClaim suggestion.
func TestActiveLearner_VerifyClaim(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	// Node created 40 days ago with high confidence
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	oldNode := makeALNode(alNS, 0.9, oldTime, map[string]any{"text": "Project deadline is Q2"})
	if err := g.UpsertNode(ctx, oldNode); err != nil {
		t.Fatalf("UpsertNode (old): %v", err)
	}

	// A newer contradicting node
	now := time.Now()
	newNode := makeALNode(alNS, 0.8, now, map[string]any{"text": "Project deadline is Q3"})
	if err := g.UpsertNode(ctx, newNode); err != nil {
		t.Fatalf("UpsertNode (new): %v", err)
	}

	// Add a contradiction edge from old → new
	edge := makeContradictEdge(alNS, oldNode.ID, newNode.ID)
	if err := g.UpsertEdge(ctx, edge); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	learner := NewActiveLearner(g)
	suggestions, err := learner.Suggest(ctx, alNS, 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	found := false
	for _, s := range suggestions {
		if s.Type == AcquireVerifyClaim {
			found = true
			if s.Priority != 0.7 {
				t.Errorf("Priority = %v, want 0.7", s.Priority)
			}
			if len(s.RelatedNodeIDs) != 1 || s.RelatedNodeIDs[0] != oldNode.ID {
				t.Errorf("RelatedNodeIDs = %v, want [%v]", s.RelatedNodeIDs, oldNode.ID)
			}
		}
	}
	if !found {
		t.Error("expected at least one AcquireVerifyClaim suggestion")
	}
}

// TestActiveLearner_BudgetLimitsOutput ensures Suggest never returns more
// suggestions than the specified budget.
func TestActiveLearner_BudgetLimitsOutput(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	now := time.Now()
	// Insert 10 low-confidence nodes — each should trigger a suggestion
	for i := 0; i < 10; i++ {
		n := makeALNode(alNS, 0.1, now, nil)
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	learner := NewActiveLearner(g)
	const budget = 3
	suggestions, err := learner.Suggest(ctx, alNS, budget)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	if len(suggestions) > budget {
		t.Errorf("got %d suggestions, want <= %d (budget)", len(suggestions), budget)
	}
}

// TestActiveLearner_PrioritySorted verifies that suggestions are ordered
// from highest priority to lowest.
func TestActiveLearner_PrioritySorted(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	now := time.Now()

	// Low confidence 0.05 → priority 0.95
	highPriNode := makeALNode(alNS, 0.05, now, map[string]any{"text": "High priority claim"})
	// Low confidence 0.35 → priority 0.65
	lowPriNode := makeALNode(alNS, 0.35, now, map[string]any{"text": "Lower priority claim"})

	for _, n := range []core.Node{highPriNode, lowPriNode} {
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	learner := NewActiveLearner(g)
	suggestions, err := learner.Suggest(ctx, alNS, 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	if len(suggestions) < 2 {
		t.Fatalf("expected at least 2 suggestions, got %d", len(suggestions))
	}

	for i := 1; i < len(suggestions); i++ {
		if suggestions[i].Priority > suggestions[i-1].Priority {
			t.Errorf("suggestions not sorted: index %d priority %v > index %d priority %v",
				i, suggestions[i].Priority, i-1, suggestions[i-1].Priority)
		}
	}
}

// TestActiveLearner_DefaultBudget ensures that a budget of 0 defaults to 10.
func TestActiveLearner_DefaultBudget(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	now := time.Now()
	// Insert 15 low-confidence nodes
	for i := 0; i < 15; i++ {
		n := makeALNode(alNS, 0.1, now, nil)
		if err := g.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}

	learner := NewActiveLearner(g)
	suggestions, err := learner.Suggest(ctx, alNS, 0) // 0 → default 10
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	if len(suggestions) > 10 {
		t.Errorf("default budget should cap at 10, got %d", len(suggestions))
	}
}

// TestActiveLearner_ExpiryOutsideWindow ensures a node expiring far in the future
// (> 7 days) does NOT generate a refresh suggestion.
func TestActiveLearner_ExpiryOutsideWindow(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	now := time.Now()
	// Expires in 30 days — well outside the 7-day window
	expiry := now.Add(30 * 24 * time.Hour)
	node := makeALNodeWithExpiry(alNS, 0.8, now, expiry, map[string]any{"text": "Future event"})
	if err := g.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	learner := NewActiveLearner(g)
	suggestions, err := learner.Suggest(ctx, alNS, 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	for _, s := range suggestions {
		if s.Type == AcquireRefreshStale {
			t.Errorf("unexpected AcquireRefreshStale for node expiring in 30 days")
		}
	}
}

// TestActiveLearner_HighConfidenceNoContradictionNotFlagged verifies that a
// high-confidence old node WITHOUT contradictions does not generate a verify suggestion.
func TestActiveLearner_HighConfidenceNoContradictionNotFlagged(t *testing.T) {
	ctx := context.Background()
	g := memory.NewGraphStore()

	// Node created 40 days ago with high confidence, no contradictions
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	node := makeALNode(alNS, 0.9, oldTime, map[string]any{"text": "Stable old fact"})
	if err := g.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	learner := NewActiveLearner(g)
	suggestions, err := learner.Suggest(ctx, alNS, 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	for _, s := range suggestions {
		if s.Type == AcquireVerifyClaim {
			t.Errorf("unexpected AcquireVerifyClaim for old node without contradictions")
		}
	}
}
