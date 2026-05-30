package retrieval_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/antiartificial/contextdb/testdata"
)

func TestRepresentativeCorpusRankingGolden(t *testing.T) {
	ctx := context.Background()
	corpus := testdata.Build()
	engine := retrieval.Engine{
		Graph:   corpus.Graph,
		Vectors: corpus.Vecs,
		KV:      corpus.KV,
	}

	for _, query := range corpus.QuerySet {
		query := query
		t.Run(query.ID, func(t *testing.T) {
			cfg := namespace.Defaults(query.Namespace, corpusMode(query.Namespace))
			params := cfg.ScoreParams

			results, err := engine.Retrieve(ctx, retrieval.Query{
				Namespace:   query.Namespace,
				Vector:      query.Vector,
				TopK:        5,
				Strategy:    retrieval.HybridStrategy{VectorWeight: 1, Traversal: cfg.Traversal, MaxDepth: cfg.MaxDepth},
				ScoreParams: params,
			})
			if err != nil {
				t.Fatalf("retrieve representative corpus: %v", err)
			}
			if len(results) == 0 {
				t.Fatalf("no results for %s", query.Description)
			}
			cutoff := expectedRankCutoff(query.Category)
			if len(results) < cutoff {
				cutoff = len(results)
			}
			if !containsAnyNode(query.CorrectNodeIDs, results[:cutoff]) {
				t.Fatalf("no expected node in top %d for %s; order=%v",
					cutoff, query.Description, resultTexts(results))
			}
		})
	}
}

func expectedRankCutoff(category string) int {
	switch category {
	case "poisoning", "temporal", "procedural":
		return 1
	default:
		return 3
	}
}

func corpusMode(ns string) namespace.Mode {
	switch ns {
	case testdata.NSChannel:
		return namespace.ModeBeliefSystem
	case testdata.NSAgent:
		return namespace.ModeAgentMemory
	case testdata.NSProcedural:
		return namespace.ModeProcedural
	default:
		return namespace.ModeGeneral
	}
}

func containsNode(ids []uuid.UUID, id uuid.UUID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func containsAnyNode(ids []uuid.UUID, results []core.ScoredNode) bool {
	for _, result := range results {
		if containsNode(ids, result.Node.ID) {
			return true
		}
	}
	return false
}

func resultTexts(results []core.ScoredNode) []string {
	texts := make([]string, len(results))
	for i, result := range results {
		text, _ := result.Node.Properties["text"].(string)
		texts[i] = text
	}
	return texts
}
