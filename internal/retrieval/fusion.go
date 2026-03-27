package retrieval

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// Query describes a hybrid retrieval request.
type Query struct {
	Namespace   string
	Vector      []float32
	Vectors     [][]float32 // multi-vector queries: fan-out and merge
	QueryText   string      // original text for reranking
	SeedIDs        []uuid.UUID
	SessionNodeIDs []uuid.UUID // IDs of recently-retrieved nodes from the current session
	TopK           int
	Labels           []string
	ExcludeSourceIDs []string // source IDs to exclude from results (counterfactual queries)
	Strategy         HybridStrategy
	ScoreParams      core.ScoreParams
}

// HybridStrategy controls the relative contribution of each retrieval path.
type HybridStrategy struct {
	VectorWeight   float64
	GraphWeight    float64
	SessionWeight  float64
	Traversal      store.TraversalStrategy
	MaxDepth       int
	DiversityLambda float64 // MMR lambda: 0 = disabled, 0.7 = typical diversity
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
	Graph    store.GraphStore
	Vectors  store.VectorIndex
	KV       store.KVStore
	Reranker Reranker // optional — if set, top results are reranked
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

	// Collect all query vectors (primary + multi-vector)
	var queryVectors [][]float32
	if len(q.Vector) > 0 {
		queryVectors = append(queryVectors, q.Vector)
	}
	queryVectors = append(queryVectors, q.Vectors...)

	// Fan out concurrently
	fanCount := len(queryVectors) + 1 // +1 for graph walk
	var wg sync.WaitGroup
	resultCh := make(chan fanResult, fanCount)

	for _, vec := range queryVectors {
		if e.Vectors != nil {
			wg.Add(1)
			go func(v []float32) {
				defer wg.Done()
				res, err := e.Vectors.Search(ctx, store.VectorQuery{
					Namespace: q.Namespace,
					Vector:    v,
					TopK:      q.TopK * 2,
					Labels:    q.Labels,
					AsOf:      q.ScoreParams.AsOf,
				})
				resultCh <- fanResult{vectorResults: res, err: err, source: "vector"}
			}(vec)
		}
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

	var allVectorResults []core.ScoredNode
	var graphResults []core.Node
	for r := range resultCh {
		if r.err != nil {
			return nil, r.err
		}
		switch r.source {
		case "vector":
			allVectorResults = append(allVectorResults, r.vectorResults...)
		case "graph":
			graphResults = r.graphResults
		}
	}

	// Look up session nodes for reweighting
	var sessionNodes []core.Node
	if len(q.SessionNodeIDs) > 0 && e.Graph != nil {
		for _, sid := range q.SessionNodeIDs {
			n, err := e.Graph.GetNode(ctx, q.Namespace, sid)
			if err == nil && n != nil {
				sessionNodes = append(sessionNodes, *n)
			}
		}
	}

	results := e.fuse(allVectorResults, graphResults, sessionNodes, q)

	// Apply MMR diversity reranking if configured
	if q.Strategy.DiversityLambda > 0 {
		results = mmrRerank(results, q.Strategy.DiversityLambda, q.TopK)
	}

	// Rerank if configured and we have a query text
	if e.Reranker != nil && q.QueryText != "" && len(results) > 0 {
		rerankInput := make([]core.Node, len(results))
		for i, r := range results {
			rerankInput[i] = r.Node
		}
		reranked, err := e.Reranker.Rerank(ctx, q.QueryText, rerankInput, q.TopK)
		if err == nil && len(reranked) > 0 {
			return reranked, nil
		}
		// On rerank failure, fall through to original results
	}

	return results, nil
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

	// Downweight candidates that are too similar to session nodes
	if len(sessionResults) > 0 {
		for _, c := range seen {
			if len(c.node.Vector) == 0 {
				continue
			}
			var maxSim float64
			for _, sn := range sessionResults {
				if len(sn.Vector) == 0 {
					continue
				}
				sim := core.CosineSimilarity(c.node.Vector, sn.Vector)
				if sim > maxSim {
					maxSim = sim
				}
			}
			// Reduce similarity score for nodes very similar to session
			// (threshold 0.8 avoids penalizing loosely related results)
			if maxSim > 0.8 {
				penalty := 1.0 - (maxSim-0.8)*2.5 // linear from 1.0 at 0.8 to 0.5 at 1.0
				if penalty < 0.5 {
					penalty = 0.5
				}
				c.similarity *= penalty
			}
		}
	}

	// Counterfactual: exclude nodes from specific sources
	if len(q.ExcludeSourceIDs) > 0 {
		excludeSet := make(map[string]bool, len(q.ExcludeSourceIDs))
		for _, id := range q.ExcludeSourceIDs {
			excludeSet[id] = true
		}
		for id, c := range seen {
			if sourceID, ok := c.node.Properties["source_id"].(string); ok && excludeSet[sourceID] {
				delete(seen, id)
			}
		}
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

// mmrRerank applies Maximal Marginal Relevance to reorder results,
// balancing relevance (original score) against diversity (vector dissimilarity
// to already-selected results). Lambda controls the trade-off: 1.0 = pure
// relevance, 0.0 = pure diversity. The function preserves original scores;
// it only changes ordering.
func mmrRerank(results []core.ScoredNode, lambda float64, topK int) []core.ScoredNode {
	if lambda <= 0 || len(results) <= 1 {
		return results
	}
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}

	selected := make([]core.ScoredNode, 0, topK)
	remaining := make([]bool, len(results)) // true = still available
	for i := range remaining {
		remaining[i] = true
	}

	// maxSimToSelected[i] tracks the maximum cosine similarity of results[i]
	// to any already-selected item. Updated incrementally after each selection
	// to avoid O(k) recomputation per candidate per iteration.
	maxSimToSelected := make([]float64, len(results))

	// Start with the highest-scoring result (results are already sorted by score).
	selected = append(selected, results[0])
	remaining[0] = false

	// Initialize maxSimToSelected against the first selected item.
	for i, avail := range remaining {
		if !avail {
			continue
		}
		sim := core.CosineSimilarity(results[i].Node.Vector, results[0].Node.Vector)
		if sim > maxSimToSelected[i] {
			maxSimToSelected[i] = sim
		}
	}

	for len(selected) < topK {
		bestIdx := -1
		bestMMR := -2.0 // scores can be negative in theory

		for i, avail := range remaining {
			if !avail {
				continue
			}
			maxSim := maxSimToSelected[i]
			// If vectors are missing, maxSim stays 0 → no diversity penalty
			mmrScore := lambda*results[i].Score - (1-lambda)*maxSim
			if mmrScore > bestMMR {
				bestMMR = mmrScore
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}
		selected = append(selected, results[bestIdx])
		remaining[bestIdx] = false

		// Update maxSimToSelected for all remaining candidates against the
		// newly selected item, so the next iteration stays O(n) not O(n*k).
		for i, avail := range remaining {
			if !avail {
				continue
			}
			sim := core.CosineSimilarity(results[i].Node.Vector, results[bestIdx].Node.Vector)
			if sim > maxSimToSelected[i] {
				maxSimToSelected[i] = sim
			}
		}
	}

	return selected
}
