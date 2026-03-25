package retrieval_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/ataraxy-labs/contextdb/internal/core"
	"github.com/ataraxy-labs/contextdb/internal/retrieval"
	memstore "github.com/ataraxy-labs/contextdb/internal/store/memory"
)

const testNS = "test:fusion"

func makeVec(seed, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		if i == seed%dim {
			v[i] = 0.9
		} else {
			v[i] = 0.1
		}
	}
	return v
}

func seedFixtures(t *testing.T, graph *memstore.GraphStore, vecs *memstore.VectorIndex) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nodes := []core.Node{
		{ID: uuid.New(), Namespace: testNS, Labels: []string{"Concept"},
			Properties: map[string]any{"text": "Go garbage collection"},
			Confidence: 0.9, ValidFrom: time.Now()},
		{ID: uuid.New(), Namespace: testNS, Labels: []string{"Concept"},
			Properties: map[string]any{"text": "Go runtime scheduler"},
			Confidence: 0.85, ValidFrom: time.Now()},
		{ID: uuid.New(), Namespace: testNS, Labels: []string{"Claim"},
			Properties: map[string]any{"text": "Go has a mark-and-sweep GC"},
			Confidence: 0.95, ValidFrom: time.Now()},
		{ID: uuid.New(), Namespace: testNS, Labels: []string{"Claim"},
			Properties: map[string]any{"text": "Python uses reference counting"},
			Confidence: 0.80, ValidFrom: time.Now()},
	}
	ids := make([]uuid.UUID, len(nodes))
	for i, n := range nodes {
		if err := graph.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert node: %v", err)
		}
		vecs.RegisterNode(n)
		nID := n.ID
		if err := vecs.Index(ctx, core.VectorEntry{
			ID: uuid.New(), Namespace: testNS, NodeID: &nID,
			Vector: makeVec(i, 8), Text: n.Properties["text"].(string),
			ModelID: "test", CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("index vector: %v", err)
		}
		ids[i] = n.ID
	}
	return ids
}

func TestEngine_VectorOnlyRetrieval(t *testing.T) {
	is := is.New(t)
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	seedFixtures(t, graph, vecs)

	engine := &retrieval.Engine{Graph: graph, Vectors: vecs, KV: kv}
	q := retrieval.Query{
		Namespace: testNS, Vector: makeVec(0, 8), TopK: 3,
		ScoreParams: core.GeneralParams(),
	}
	q.ScoreParams.AsOf = time.Now()

	results, err := engine.Retrieve(context.Background(), q)
	is.NoErr(err)
	is.True(len(results) > 0)
	is.True(len(results) <= 3)
	for _, r := range results {
		t.Logf("  [%s] score=%.4f sim=%.4f conf=%.2f",
			r.Properties["text"], r.Score, r.SimilarityScore, r.Confidence)
	}
}

func TestEngine_GraphWalkRetrieval(t *testing.T) {
	is := is.New(t)
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	ctx := context.Background()
	ids := seedFixtures(t, graph, vecs)

	if err := graph.UpsertEdge(ctx, core.Edge{
		ID: uuid.New(), Namespace: testNS,
		Src: ids[0], Dst: ids[2], Type: "relates_to",
		Weight: 0.8, ValidFrom: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	engine := &retrieval.Engine{Graph: graph, Vectors: vecs, KV: kv}
	q := retrieval.Query{
		Namespace: testNS, SeedIDs: []uuid.UUID{ids[0]}, TopK: 5,
		ScoreParams: core.GeneralParams(),
	}
	q.ScoreParams.AsOf = time.Now()

	results, err := engine.Retrieve(ctx, q)
	is.NoErr(err)
	is.True(len(results) > 0)
	inResults := map[uuid.UUID]bool{}
	for _, r := range results {
		inResults[r.ID] = true
	}
	is.True(inResults[ids[0]] || inResults[ids[2]])
}

func TestEngine_StrategyComparison(t *testing.T) {
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	ctx := context.Background()
	ids := seedFixtures(t, graph, vecs)

	_ = graph.UpsertEdge(ctx, core.Edge{
		ID: uuid.New(), Namespace: testNS,
		Src: ids[0], Dst: ids[1], Type: "relates_to",
		Weight: 1.0, ValidFrom: time.Now(),
	})

	strategies := []struct {
		name     string
		strategy retrieval.HybridStrategy
		params   core.ScoreParams
	}{
		{"BeliefSystem", retrieval.HybridStrategy{VectorWeight: 0.30, GraphWeight: 0.55, SessionWeight: 0.15, MaxDepth: 3}, core.BeliefSystemParams()},
		{"AgentMemory", retrieval.HybridStrategy{VectorWeight: 0.50, GraphWeight: 0.30, SessionWeight: 0.20, MaxDepth: 2}, core.AgentMemoryParams()},
		{"General", retrieval.HybridStrategy{VectorWeight: 0.45, GraphWeight: 0.40, SessionWeight: 0.15, MaxDepth: 3}, core.GeneralParams()},
	}

	engine := &retrieval.Engine{Graph: graph, Vectors: vecs, KV: kv}
	t.Log("\n=== Strategy Comparison ===")
	t.Logf("%-20s %-30s %6s %6s %6s", "strategy", "text", "score", "sim", "conf")
	t.Log("----------------------------------------------------------------------")

	for _, s := range strategies {
		s.params.AsOf = time.Now()
		q := retrieval.Query{
			Namespace: testNS, Vector: makeVec(0, 8),
			SeedIDs: []uuid.UUID{ids[0]}, TopK: 4,
			Strategy: s.strategy, ScoreParams: s.params,
		}
		results, err := engine.Retrieve(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			text, _ := r.Properties["text"].(string)
			if len(text) > 28 {
				text = text[:28]
			}
			t.Logf("%-20s %-30s %6.4f %6.4f %6.2f",
				s.name, text, r.Score, r.SimilarityScore, r.Confidence)
		}
		t.Log("---")
	}
}

func TestEngine_TopKRespected(t *testing.T) {
	is := is.New(t)
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	seedFixtures(t, graph, vecs)
	engine := &retrieval.Engine{Graph: graph, Vectors: vecs, KV: kv}

	for _, topK := range []int{1, 2, 3} {
		q := retrieval.Query{
			Namespace: testNS, Vector: makeVec(0, 8), TopK: topK,
			ScoreParams: core.GeneralParams(),
		}
		q.ScoreParams.AsOf = time.Now()
		results, err := engine.Retrieve(context.Background(), q)
		is.NoErr(err)
		is.True(len(results) <= topK)
	}
}

func TestEngine_EmptyStoreReturnsEmpty(t *testing.T) {
	is := is.New(t)
	engine := &retrieval.Engine{
		Graph:   memstore.NewGraphStore(),
		Vectors: memstore.NewVectorIndex(),
		KV:      memstore.NewKVStore(),
	}
	q := retrieval.Query{
		Namespace: "empty", Vector: makeVec(0, 8), TopK: 10,
		ScoreParams: core.GeneralParams(),
	}
	q.ScoreParams.AsOf = time.Now()
	results, err := engine.Retrieve(context.Background(), q)
	is.NoErr(err)
	is.Equal(0, len(results))
}
