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

func TestConflictDetection(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewConflictDetector(graph, nil) // heuristic mode

	// Create an existing node
	existingID := uuid.New()
	existing := core.Node{
		ID:         existingID,
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "The sky is blue"},
		Confidence: 0.9,
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}
	err := graph.UpsertNode(ctx, existing)
	is.NoErr(err)

	// Create a contradicting candidate
	candidateID := uuid.New()
	candidate := core.Node{
		ID:         candidateID,
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "The sky is green"},
		Confidence: 0.8,
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}

	// Simulate nearest neighbour with moderate similarity
	nearest := []core.ScoredNode{
		{
			Node:            existing,
			SimilarityScore: 0.7, // moderate — same topic but different
			RetrievalSource: "vector",
		},
	}

	result, err := detector.Detect(ctx, candidate, nearest)
	is.NoErr(err)
	is.True(len(result.ConflictIDs) > 0) // should detect contradiction
	is.Equal(result.ConflictIDs[0], existingID)

	// Verify contradicts edge was created
	edges, err := graph.EdgesFrom(ctx, "test", candidateID, []string{"contradicts"})
	is.NoErr(err)
	is.Equal(len(edges), 1)
	is.Equal(edges[0].Type, "contradicts")
	is.Equal(edges[0].Dst, existingID)
}

func TestNoConflictHighSimilarity(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewConflictDetector(graph, nil)

	candidate := core.Node{
		ID:         uuid.New(),
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "The sky is blue"},
	}

	// Very high similarity = near-duplicate, not a contradiction
	nearest := []core.ScoredNode{
		{
			Node: core.Node{
				ID:         uuid.New(),
				Namespace:  "test",
				Labels:     []string{"Claim"},
				Properties: map[string]any{"text": "The sky is blue"},
			},
			SimilarityScore: 0.98,
		},
	}

	result, err := detector.Detect(ctx, candidate, nearest)
	is.NoErr(err)
	is.Equal(len(result.ConflictIDs), 0) // not a contradiction
}

func TestNoConflictDifferentLabels(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	detector := NewConflictDetector(graph, nil)

	candidate := core.Node{
		ID:         uuid.New(),
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "The sky is blue"},
	}

	nearest := []core.ScoredNode{
		{
			Node: core.Node{
				ID:         uuid.New(),
				Namespace:  "test",
				Labels:     []string{"Skill"}, // different label
				Properties: map[string]any{"text": "Deploy a server"},
			},
			SimilarityScore: 0.7,
		},
	}

	result, err := detector.Detect(ctx, candidate, nearest)
	is.NoErr(err)
	is.Equal(len(result.ConflictIDs), 0)
}

func TestLabelOverlapRatio(t *testing.T) {
	is := is.New(t)

	is.Equal(labelOverlapRatio([]string{"A", "B"}, []string{"B", "C"}), 1.0/3.0)
	is.Equal(labelOverlapRatio([]string{"A"}, []string{"A"}), 1.0)
	is.Equal(labelOverlapRatio([]string{"A"}, []string{"B"}), 0.0)
	is.Equal(labelOverlapRatio(nil, []string{"A"}), 0.0)
	is.Equal(labelOverlapRatio([]string{}, []string{"A"}), 0.0)
}
