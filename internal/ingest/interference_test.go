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

// buildNode is a helper to create a minimal node for interference tests.
func buildNode(ns string, conf float64) core.Node {
	return core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "some claim"},
		Confidence: conf,
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}
}

// addSupporter upserts a supporting edge pointing at targetID.
func addSupporter(t *testing.T, graph *memstore.GraphStore, ns string, targetID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	supporterNode := buildNode(ns, 0.9)
	if err := graph.UpsertNode(ctx, supporterNode); err != nil {
		t.Fatalf("upsert supporter node: %v", err)
	}
	edge := core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       supporterNode.ID,
		Dst:       targetID,
		Type:      core.EdgeSupports,
		Weight:    1.0,
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}
	if err := graph.UpsertEdge(ctx, edge); err != nil {
		t.Fatalf("upsert support edge: %v", err)
	}
}

// TestInterference_LowCredibilityVsWellEstablished verifies that a
// low-confidence candidate contradicting a high-confidence, well-supported
// node is flagged as interference.
func TestInterference_LowCredibilityVsWellEstablished(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewInterferenceDetector(graph)

	existing := buildNode("test", 0.9)
	if err := graph.UpsertNode(ctx, existing); err != nil {
		t.Fatalf("upsert existing: %v", err)
	}

	// Add two supporters so the existing node has "strong evidence"
	addSupporter(t, graph, "test", existing.ID)
	addSupporter(t, graph, "test", existing.ID)

	candidate := buildNode("test", 0.2) // low-credibility

	result := detector.Check(ctx, "test", candidate, existing)
	is.True(result.IsInterference)
	is.True(result.Reason != "")
}

// TestInterference_EquallyConfidentNodes verifies that two nodes with similar
// confidence levels are NOT flagged as interference.
func TestInterference_EquallyConfidentNodes(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewInterferenceDetector(graph)

	existing := buildNode("test", 0.8)
	if err := graph.UpsertNode(ctx, existing); err != nil {
		t.Fatalf("upsert existing: %v", err)
	}

	addSupporter(t, graph, "test", existing.ID)
	addSupporter(t, graph, "test", existing.ID)

	candidate := buildNode("test", 0.75) // reasonably credible — not low-confidence

	result := detector.Check(ctx, "test", candidate, existing)
	is.True(!result.IsInterference)
}

// TestInterference_HighConfidenceNoSupporters verifies that a high-confidence
// existing node WITHOUT sufficient supporters is not treated as interference,
// since the evidence base is weak.
func TestInterference_HighConfidenceNoSupporters(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewInterferenceDetector(graph)

	existing := buildNode("test", 0.9) // high confidence
	if err := graph.UpsertNode(ctx, existing); err != nil {
		t.Fatalf("upsert existing: %v", err)
	}
	// Only one supporter — below the threshold of 2
	addSupporter(t, graph, "test", existing.ID)

	candidate := buildNode("test", 0.2) // low-credibility candidate

	result := detector.Check(ctx, "test", candidate, existing)
	is.True(!result.IsInterference) // not enough evidence despite high confidence
}

// TestInterference_SkipsDecay_Integration verifies that when interference is
// detected, the existing node's confidence is NOT decayed by the ConflictDetector.
func TestInterference_SkipsDecay_Integration(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	conflictDetector := NewConflictDetector(graph, nil)

	existing := core.Node{
		ID:         uuid.New(),
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "The sky is blue"},
		Confidence: 0.9,
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}
	if err := graph.UpsertNode(ctx, existing); err != nil {
		t.Fatalf("upsert existing: %v", err)
	}

	// Two supporters — enough for interference detection
	addSupporter(t, graph, "test", existing.ID)
	addSupporter(t, graph, "test", existing.ID)

	// Low-credibility candidate with different text — triggers heuristic contradiction
	candidate := core.Node{
		ID:         uuid.New(),
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "The sky is green"},
		Confidence: 0.2,
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}

	nearest := []core.ScoredNode{
		{
			Node:            existing,
			SimilarityScore: 0.7, // moderate — contradiction candidate range
		},
	}

	result, err := conflictDetector.Detect(ctx, candidate, nearest)
	is.NoErr(err)
	is.True(len(result.ConflictIDs) > 0) // contradiction still recorded

	// The existing node's confidence should NOT have decayed
	updated, err := graph.GetNode(ctx, "test", existing.ID)
	is.NoErr(err)
	is.Equal(updated.Confidence, existing.Confidence) // unchanged
}
