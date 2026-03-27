package dsl

import (
	"testing"
	"time"
)

func TestToRetrieveRequestBasic(t *testing.T) {
	q := &Query{
		SearchText: "project status",
		Namespace:  "agent_memory",
		Weights: ScoreWeights{
			Similarity: 0.4,
			Confidence: 0.2,
			Recency:    0.4,
			Utility:    NoWeight,
		},
		Limit:  10,
		Rerank: true,
	}

	req := ToRetrieveRequest(q)

	if req.Text != "project status" {
		t.Errorf("Text = %q", req.Text)
	}
	if req.TopK != 10 {
		t.Errorf("TopK = %d", req.TopK)
	}
	if req.ScoreParams.SimilarityWeight != 0.4 {
		t.Errorf("SimilarityWeight = %v", req.ScoreParams.SimilarityWeight)
	}
	if req.ScoreParams.RecencyWeight != 0.4 {
		t.Errorf("RecencyWeight = %v", req.ScoreParams.RecencyWeight)
	}
}

func TestToRetrieveRequestTemporal(t *testing.T) {
	asOf := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	q := &Query{
		SearchText: "test",
		ValidAt:    &asOf,
		Weights:    DefaultWeights(),
	}

	req := ToRetrieveRequest(q)

	if !req.AsOf.Equal(asOf) {
		t.Errorf("AsOf = %v", req.AsOf)
	}
}

func TestToRetrieveRequestLabels(t *testing.T) {
	q := &Query{
		SearchText: "test",
		Weights:    DefaultWeights(),
		Predicates: []Predicate{
			{Field: "label", Op: OpEq, Value: Value{Type: ValString, Str: "hr"}},
			{Field: "label", Op: OpIn, Value: Value{Type: ValStringList, Strings: []string{"org", "team"}}},
			{Field: "confidence", Op: OpGt, Value: Value{Type: ValNumber, Num: 0.5}},
		},
	}

	req := ToRetrieveRequest(q)

	if len(req.Labels) != 3 {
		t.Fatalf("got %d labels, want 3", len(req.Labels))
	}
}

func TestToRetrieveRequestGraph(t *testing.T) {
	q := &Query{
		SearchText: "test",
		Weights:    DefaultWeights(),
		Graph: &GraphOpts{
			Edges: []EdgeSpec{
				{Type: "contradicts", MaxDepth: 3},
			},
		},
	}

	req := ToRetrieveRequest(q)

	if req.Strategy.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d", req.Strategy.MaxDepth)
	}
	if req.Strategy.GraphWeight == 0 {
		t.Error("expected nonzero GraphWeight")
	}
}
