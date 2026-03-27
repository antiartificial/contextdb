package dsl

import (
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
)

// ToRetrieveRequest converts a parsed Query AST into a client.RetrieveRequest.
// Fields not expressible in the DSL (Vector, Vectors, SeedIDs) remain at
// zero values — the caller must fill those in from context.
func ToRetrieveRequest(q *Query) client.RetrieveRequest {
	req := client.RetrieveRequest{
		Text: q.SearchText,
		TopK: q.Limit,
	}

	// Temporal
	if q.ValidAt != nil {
		req.AsOf = *q.ValidAt
		req.ScoreParams.AsOf = *q.ValidAt
	}

	// Score weights
	req.ScoreParams = buildScoreParams(q)

	// Graph traversal
	if q.Graph != nil && len(q.Graph.Edges) > 0 {
		req.Strategy = buildStrategy(q)
	}

	// Labels extracted from predicates
	req.Labels = extractLabels(q.Predicates)

	return req
}

func buildScoreParams(q *Query) core.ScoreParams {
	p := core.ScoreParams{}
	if q.Weights.Similarity >= 0 {
		p.SimilarityWeight = q.Weights.Similarity
	}
	if q.Weights.Confidence >= 0 {
		p.ConfidenceWeight = q.Weights.Confidence
	}
	if q.Weights.Recency >= 0 {
		p.RecencyWeight = q.Weights.Recency
	}
	if q.Weights.Utility >= 0 {
		p.UtilityWeight = q.Weights.Utility
	}
	if q.ValidAt != nil {
		p.AsOf = *q.ValidAt
	}
	return p
}

func buildStrategy(q *Query) retrieval.HybridStrategy {
	s := retrieval.HybridStrategy{
		VectorWeight: 0.45,
		GraphWeight:  0.40,
	}

	maxDepth := 0
	var edgeTypes []string
	for _, e := range q.Graph.Edges {
		edgeTypes = append(edgeTypes, e.Type)
		if e.MaxDepth > maxDepth {
			maxDepth = e.MaxDepth
		}
	}
	if maxDepth == 0 {
		maxDepth = 2
	}
	s.MaxDepth = maxDepth
	s.Traversal = store.StrategyWaterCircle

	// Edge types are stored in the traversal strategy; the current
	// HybridStrategy doesn't have an EdgeTypes field, so we rely on
	// the retrieval engine's default traversal filtering.
	// TODO: plumb edge type filter through to WalkQuery.EdgeTypes
	_ = edgeTypes

	return s
}

// extractLabels pulls "label" predicates out as string labels.
func extractLabels(preds []Predicate) []string {
	var labels []string
	for _, p := range preds {
		if p.Field != "label" {
			continue
		}
		switch p.Op {
		case OpEq:
			if p.Value.Type == ValString {
				labels = append(labels, p.Value.Str)
			}
		case OpIn:
			labels = append(labels, p.Value.Strings...)
		}
	}
	return labels
}
