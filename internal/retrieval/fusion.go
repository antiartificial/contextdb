package retrieval

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// Query describes a hybrid retrieval request.
type Query struct {
	Namespace        string
	Vector           []float32
	Vectors          [][]float32 // multi-vector queries: fan-out and merge
	QueryText        string      // original text for reranking
	SeedIDs          []uuid.UUID
	SessionNodeIDs   []uuid.UUID // IDs of recently-retrieved nodes from the current session
	TopK             int
	Labels           []string
	IncludeSourceIDs []string // return nodes supported by at least one of these sources
	ExcludeSourceIDs []string // source IDs to exclude from results (counterfactual queries)
	Strategy         HybridStrategy
	ScoreParams      core.ScoreParams
	KnownAt          time.Time // transaction-time cutoff; zero defaults to now
}

// HybridStrategy controls the relative contribution of each retrieval path.
type HybridStrategy struct {
	VectorWeight  float64
	GraphWeight   float64
	SessionWeight float64
	Traversal     store.TraversalStrategy
	MaxDepth      int
	// EdgeTypes restricts graph traversal to the listed edge types. A nil or
	// empty slice traverses every edge type.
	EdgeTypes       []string
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
	if q.Strategy.IsZero() {
		q.Strategy = defaultStrategy()
	}
	if q.TopK <= 0 {
		q.TopK = 20
	}
	if q.ScoreParams.AsOf.IsZero() {
		q.ScoreParams.AsOf = time.Now()
	}
	if q.KnownAt.IsZero() {
		q.KnownAt = time.Now()
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
				candidateK := q.TopK * 4
				if candidateK < 50 {
					candidateK = 50
				}
				res, err := e.Vectors.Search(ctx, store.VectorQuery{
					Namespace: q.Namespace,
					Vector:    v,
					TopK:      candidateK,
					Labels:    q.Labels,
					AsOf:      q.ScoreParams.AsOf,
					KnownAt:   q.KnownAt,
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
				EdgeTypes: q.Strategy.EdgeTypes,
				MaxDepth:  q.Strategy.MaxDepth,
				Strategy:  q.Strategy.Traversal,
				AsOf:      q.ScoreParams.AsOf,
				KnownAt:   q.KnownAt,
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
			hydrated, err := e.hydrateVectorResults(ctx, q, r.vectorResults)
			if err != nil {
				return nil, err
			}
			allVectorResults = append(allVectorResults, hydrated...)
		case "graph":
			graphResults = r.graphResults
		}
	}

	// Look up session nodes for reweighting
	var sessionNodes []core.Node
	if len(q.SessionNodeIDs) > 0 && e.Graph != nil {
		for _, sid := range q.SessionNodeIDs {
			n, err := e.Graph.At(ctx, q.Namespace, sid, q.ScoreParams.AsOf, q.KnownAt)
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

// hydrateVectorResults replaces the vector index's cached node with the graph
// version valid at the query anchor. Vector indexes intentionally cache nodes
// for ANN assembly, so graph-only feedback and retractions must be refreshed
// before confidence scoring and fusion.
func (e *Engine) hydrateVectorResults(ctx context.Context, q Query, candidates []core.ScoredNode) ([]core.ScoredNode, error) {
	if e.Graph == nil || len(candidates) == 0 {
		return candidates, nil
	}
	result := make([]core.ScoredNode, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Node.ID == uuid.Nil {
			continue
		}
		node, err := e.Graph.At(ctx, q.Namespace, candidate.Node.ID, q.ScoreParams.AsOf, q.KnownAt)
		if err != nil {
			return nil, fmt.Errorf("hydrate vector candidate %s at valid/known time: %w", candidate.Node.ID, err)
		}
		if node == nil || !node.IsValidAt(q.ScoreParams.AsOf) || !matchesLabels(*node, q.Labels) {
			continue
		}
		// Graph backends may store vector entries separately. Preserve the ANN
		// vector only for similarity/MMR while all mutable metadata is hydrated.
		if len(node.Vector) == 0 {
			node.Vector = candidate.Node.Vector
		}
		candidate.Node = *node
		result = append(result, candidate)
	}
	return result, nil
}

func matchesLabels(node core.Node, labels []string) bool {
	for _, label := range labels {
		if !node.HasLabel(label) {
			return false
		}
	}
	return true
}

// IsZero reports whether no retrieval strategy fields were configured.
func (s HybridStrategy) IsZero() bool {
	return s.VectorWeight == 0 &&
		s.GraphWeight == 0 &&
		s.SessionWeight == 0 &&
		s.Traversal == "" &&
		s.MaxDepth == 0 &&
		len(s.EdgeTypes) == 0 &&
		s.DiversityLambda == 0
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
		seen[n.ID] = &candidate{node: n, similarity: sim, utility: nodeUtility(n), source: source}
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

	// Source constraints are applied to evidence identities. A corroborated
	// claim survives exclusion of one source when another source still supports it.
	if len(q.IncludeSourceIDs) > 0 || len(q.ExcludeSourceIDs) > 0 {
		includeSet := make(map[string]bool, len(q.IncludeSourceIDs))
		for _, id := range q.IncludeSourceIDs {
			includeSet[id] = true
		}
		excludeSet := make(map[string]bool, len(q.ExcludeSourceIDs))
		for _, id := range q.ExcludeSourceIDs {
			excludeSet[id] = true
		}
		for id, c := range seen {
			projected, matched := projectSourceEvidence(c.node, includeSet, excludeSet)
			if !matched {
				delete(seen, id)
			} else {
				c.node = projected
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

// projectSourceEvidence changes only the query result. Stored node versions
// remain intact, while counterfactual scoring uses the surviving evidence.
func projectSourceEvidence(n core.Node, include, exclude map[string]bool) (core.Node, bool) {
	allowed := func(sourceID string) bool {
		return sourceID != "" && !exclude[sourceID] && (len(include) == 0 || include[sourceID])
	}
	primary, _ := n.Properties["source_id"].(string)
	hadSources := primary != ""
	primaryAllowed := allowed(primary)
	selectedSource := ""
	selectedConfidence := -1.0
	if primaryAllowed {
		selectedSource = primary
		selectedConfidence = n.Confidence
	}
	var selectedRecords []any
	if records, ok := n.Properties["corroborating_sources"].([]any); ok {
		for _, item := range records {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			sourceID, _ := record["source_id"].(string)
			if sourceID != "" {
				hadSources = true
			}
			if !allowed(sourceID) {
				continue
			}
			selectedRecords = append(selectedRecords, item)
			if confidence, ok := record["confidence"].(float64); ok && confidence > selectedConfidence {
				selectedSource = sourceID
				selectedConfidence = confidence
			} else if selectedSource == "" {
				selectedSource = sourceID
			}
		}
	}
	if selectedSource == "" {
		// Preserve the prior behavior for nodes with no source when only an
		// exclusion is requested; inclusion requires affirmative evidence.
		return n, !hadSources && len(include) == 0
	}
	props := make(map[string]any, len(n.Properties)+1)
	for key, value := range n.Properties {
		props[key] = value
	}
	if selectedSource != primary {
		props["canonical_source_id"] = primary
	}
	props["source_id"] = selectedSource
	if len(selectedRecords) > 0 {
		props["corroborating_sources"] = selectedRecords
	} else {
		delete(props, "corroborating_sources")
	}
	n.Properties = props
	if selectedConfidence >= 0 {
		n.Confidence = selectedConfidence
	}
	return n, true
}

func nodeUtility(n core.Node) float64 {
	if n.Properties == nil {
		return 1.0
	}
	switch v := n.Properties["utility"].(type) {
	case float64:
		return clamp01(v)
	case float32:
		return clamp01(float64(v))
	case int:
		return clamp01(float64(v))
	case int64:
		return clamp01(float64(v))
	default:
		return 1.0
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
