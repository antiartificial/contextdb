package retrieval

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ataraxy-labs/contextdb/internal/core"
	"github.com/ataraxy-labs/contextdb/internal/store"
)

// Query describes a hybrid retrieval request.
type Query struct {
	Namespace   string
	Vector      []float32
	SeedIDs     []uuid.UUID
	TopK        int
	Strategy    HybridStrategy
	ScoreParams core.ScoreParams
}

// HybridStrategy controls the relative contribution of each retrieval path.
type HybridStrategy struct {
	VectorWeight  float64
	GraphWeight   float64
	SessionWeight float64
	Traversal     store.TraversalStrategy
	MaxDepth      int
}

func defaultStrategy() HybridStrategy {
	return HybridStrategy{
		VectorWeight:  0.45,
		GraphWeight:   0.40,
		SessionWeight: 0.15,
		Traversal:     store.StrategyWaterCircle,
		MaxDepth:      3,
	}
}

// Engine executes hybrid retrieval across graph, vector, and KV stores.
type Engine struct {
	Graph   store.GraphStore
	Vectors store.VectorIndex
	KV      store.KVStore
}

type fanResult struct {
	vectorResults []core.ScoredNode
	graphResults  []core.Node
	err           error
	source        string
}

// Retrieve fans out to all backends concurrently then fuses results.
func (e *Engine) Retrieve(ctx context.Context, q Query) ([]core.ScoredNode, error) {
	if q.Strategy == (HybridStrategy{}) {
		q.Strategy = defaultStrategy()
	}
	if q.TopK <= 0 {
		q.TopK = 20
	}
	if q.ScoreParams.AsOf.IsZero() {
		q.ScoreParams.AsOf = time.Now()
	}

	var wg sync.WaitGroup
	resultCh := make(chan fanResult, 3)

	if len(q.Vector) > 0 && e.Vectors != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := e.Vectors.Search(ctx, store.VectorQuery{
				Namespace: q.Namespace,
				Vector:    q.Vector,
				TopK:      q.TopK * 2,
				AsOf:      q.ScoreParams.AsOf,
			})
			resultCh <- fanResult{vectorResults: res, err: err, source: "vector"}
		}()
	}

	if len(q.SeedIDs) > 0 && e.Graph != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := e.Graph.Walk(ctx, store.WalkQuery{
				Namespace: q.Namespace,
				SeedIDs:   q.SeedIDs,
				MaxDepth:  q.Strategy.MaxDepth,
				Strategy:  q.Strategy.Traversal,
				AsOf:      q.ScoreParams.AsOf,
			})
			resultCh <- fanResult{graphResults: res, err: err, source: "graph"}
		}()
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var vectorResults []core.ScoredNode
	var graphResults []core.Node
	for r := range resultCh {
		if r.err != nil {
			return nil, r.err
		}
		switch r.source {
		case "vector":
			vectorResults = r.vectorResults
		case "graph":
			graphResults = r.graphResults
		}
	}

	return e.fuse(vectorResults, graphResults, nil, q), nil
}

func (e *Engine) fuse(
	vectorResults []core.ScoredNode,
	graphResults []core.Node,
	sessionResults []core.Node,
	q Query,
) []core.ScoredNode {
	type candidate struct {
		node       core.Node
		similarity float64
		utility    float64
		source     string
	}

	seen := make(map[uuid.UUID]*candidate)

	merge := func(n core.Node, sim float64, source string) {
		if n.ID == uuid.Nil {
			return
		}
		if existing, ok := seen[n.ID]; ok {
			if sim > existing.similarity {
				existing.similarity = sim
				existing.source = source + "+" + existing.source
			}
			return
		}
		seen[n.ID] = &candidate{node: n, similarity: sim, utility: 1.0, source: source}
	}

	for _, r := range vectorResults {
		merge(r.Node, r.SimilarityScore*q.Strategy.VectorWeight, "vector")
	}
	for _, n := range graphResults {
		merge(n, q.Strategy.GraphWeight, "graph")
	}
	for _, n := range sessionResults {
		merge(n, q.Strategy.SessionWeight, "session")
	}

	result := make([]core.ScoredNode, 0, len(seen))
	for _, c := range seen {
		sn := core.ScoreNode(c.node, c.similarity, c.utility, q.ScoreParams)
		sn.RetrievalSource = c.source
		if sn.Score > 0 {
			result = append(result, sn)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})
	if len(result) > q.TopK {
		result = result[:q.TopK]
	}
	return result
}
