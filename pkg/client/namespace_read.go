package client

import (
	"context"
	"fmt"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/google/uuid"
	"time"
)

// GapRequest configures knowledge-gap detection for a namespace.
type GapRequest struct {
	TopK       int
	MinGapSize float64
	MaxGaps    int
}

// RetrieveRequest describes a single retrieval operation.
type RetrieveRequest struct {
	// Vector is the query embedding. If nil and Text is set with an
	// Embedder configured, the text will be auto-embedded.
	Vector []float32

	// Text is a natural-language query string. When an Embedder is
	// configured and Vector is nil, this text is auto-embedded to
	// produce the query vector.
	Text string

	// Vectors allows multi-vector queries. Results from all vectors
	// are fused together with the primary Vector.
	Vectors [][]float32

	// SeedIDs are known relevant node IDs for graph traversal.
	// Optional — if empty, only vector search is used.
	SeedIDs []uuid.UUID

	// TopK is the maximum number of results to return. Default: 10.
	TopK int

	// Labels restricts results to nodes carrying all specified labels.
	Labels []string

	// ScoreParams overrides the namespace default scoring strategy.
	// Zero value uses namespace defaults.
	ScoreParams core.ScoreParams

	// Strategy overrides the namespace default retrieval strategy.
	Strategy retrieval.HybridStrategy

	// AsOf pins retrieval to a historical time (temporal query).
	// Zero value = now.
	AsOf time.Time

	// ExcludeSourceIDs filters out nodes from specific sources.
	// Supports counterfactual queries: "what if source X didn't exist?"
	ExcludeSourceIDs []string
}

// Retrieve runs a hybrid retrieval query against the namespace.
func (h *NamespaceHandle) Retrieve(ctx context.Context, req RetrieveRequest) ([]Result, error) {
	start := time.Now()
	h.db.metrics.RetrievalTotal.Inc()

	// Auto-embed text query if no vector provided and embedder is configured
	if len(req.Vector) == 0 && req.Text != "" && h.db.opts.Embedder != nil {
		vecs, err := h.db.opts.Embedder.Embed(ctx, []string{req.Text})
		if err != nil {
			return nil, fmt.Errorf("retrieve: auto-embed query: %w", err)
		}
		if len(vecs) > 0 {
			req.Vector = vecs[0]
		}
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}

	params := req.ScoreParams
	if params == (core.ScoreParams{}) {
		params = h.cfg.ScoreParams
	}
	if req.AsOf.IsZero() {
		params.AsOf = time.Now()
	} else {
		params.AsOf = req.AsOf
	}

	strategy := req.Strategy
	if strategy.IsZero() {
		strategy = retrieval.HybridStrategy{
			VectorWeight:  0.45,
			GraphWeight:   0.40,
			SessionWeight: 0.15,
			Traversal:     h.cfg.Traversal,
			MaxDepth:      h.cfg.MaxDepth,
		}
	}

	q := retrieval.Query{
		Namespace:        h.cfg.ID,
		Vector:           req.Vector,
		Vectors:          req.Vectors,
		QueryText:        req.Text,
		SeedIDs:          req.SeedIDs,
		TopK:             topK,
		Labels:           req.Labels,
		ExcludeSourceIDs: req.ExcludeSourceIDs,
		Strategy:         strategy,
		ScoreParams:      params,
	}

	scored, err := h.engine.Retrieve(ctx, q)
	if err != nil {
		h.db.metrics.RetrievalErrors.Inc()
		return nil, fmt.Errorf("retrieve: %w", err)
	}

	h.db.metrics.RetrievalLatency.ObserveDuration(time.Since(start))
	h.db.metrics.RetrievalResults.Set(float64(len(scored)))
	if len(scored) > 0 {
		h.db.metrics.RetrievalTopScore.Set(scored[0].Score)
	}

	results := make([]Result, len(scored))
	for i, sn := range scored {
		results[i] = Result{
			Node:            sn.Node,
			Score:           sn.Score,
			SimilarityScore: sn.SimilarityScore,
			ConfidenceScore: sn.ConfidenceScore,
			RecencyScore:    sn.RecencyScore,
			UtilityScore:    sn.UtilityScore,
			Breakdown:       sn.Breakdown,
			RetrievalSource: sn.RetrievalSource,
		}
		switch sn.RetrievalSource {
		case "vector":
			h.db.metrics.VectorHits.Inc()
		case "graph":
			h.db.metrics.GraphHits.Inc()
		default:
			h.db.metrics.FusedHits.Inc()
		}
	}

	return results, nil
}

// Explain returns a narrative report explaining what is known about a node.
func (h *NamespaceHandle) Explain(ctx context.Context, nodeID uuid.UUID) (*retrieval.NarrativeReport, error) {
	formatter := retrieval.NewNarrativeFormatter(h.db.graph, h.db.vecs)
	return formatter.Explain(ctx, h.cfg.ID, nodeID)
}

// KnowledgeGaps detects sparse semantic regions in this namespace.
func (h *NamespaceHandle) KnowledgeGaps(ctx context.Context, req GapRequest) (*retrieval.GapReport, error) {
	detector := retrieval.NewGapDetector(h.db.graph, h.db.vecs)
	gaps, err := detector.DetectGaps(ctx, h.cfg.ID, retrieval.GapQuery{
		TopK:       req.TopK,
		MinGapSize: req.MinGapSize,
		MaxGaps:    req.MaxGaps,
	})
	if err != nil {
		return nil, err
	}
	nodes, err := h.db.graph.ValidAt(ctx, h.cfg.ID, time.Now(), nil)
	if err != nil {
		return nil, err
	}
	return retrieval.BuildGapReport(h.cfg.ID, gaps, len(nodes)), nil
}

// GetNode retrieves a single node by ID from this namespace.
func (h *NamespaceHandle) GetNode(ctx context.Context, id uuid.UUID) (*core.Node, error) {
	return h.db.graph.GetNode(ctx, h.cfg.ID, id)
}

// WalkResult holds a node discovered during graph traversal together
// with the depth at which it was found and the full path (as node IDs)
// from the seed to this node.
type WalkResult struct {
	Node  core.Node
	Depth int
	Path  []uuid.UUID // node IDs from seed to this node
}

// Walk performs a breadth-first graph traversal from the given seed nodes.
// It uses EdgesFrom to expand outward level-by-level so that per-node
// depth and path information can be tracked (the lower-level store.Walk
// method returns flat results without this metadata).
func (h *NamespaceHandle) Walk(ctx context.Context, seedIDs []uuid.UUID, maxDepth int) ([]WalkResult, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	type entry struct {
		id   uuid.UUID
		path []uuid.UUID
	}

	visited := make(map[uuid.UUID]bool, len(seedIDs))
	var results []WalkResult

	// Initialise the frontier with the seed nodes.
	queue := make([]entry, 0, len(seedIDs))
	for _, sid := range seedIDs {
		if visited[sid] {
			continue
		}
		visited[sid] = true
		queue = append(queue, entry{id: sid, path: []uuid.UUID{sid}})

		node, err := h.db.graph.GetNode(ctx, h.cfg.ID, sid)
		if err != nil {
			return nil, fmt.Errorf("Walk: get seed %s: %w", sid, err)
		}
		if node != nil {
			results = append(results, WalkResult{
				Node:  *node,
				Depth: 0,
				Path:  []uuid.UUID{sid},
			})
		}
	}

	for depth := 1; depth <= maxDepth; depth++ {
		var nextQueue []entry
		for _, cur := range queue {
			edges, err := h.db.graph.EdgesFrom(ctx, h.cfg.ID, cur.id, nil)
			if err != nil {
				return nil, fmt.Errorf("Walk: edges from %s at depth %d: %w", cur.id, depth, err)
			}
			for _, e := range edges {
				if visited[e.Dst] {
					continue
				}
				visited[e.Dst] = true

				newPath := make([]uuid.UUID, len(cur.path)+1)
				copy(newPath, cur.path)
				newPath[len(cur.path)] = e.Dst

				node, err := h.db.graph.GetNode(ctx, h.cfg.ID, e.Dst)
				if err != nil {
					return nil, fmt.Errorf("Walk: get node %s at depth %d: %w", e.Dst, depth, err)
				}
				if node != nil {
					results = append(results, WalkResult{
						Node:  *node,
						Depth: depth,
						Path:  newPath,
					})
				}
				nextQueue = append(nextQueue, entry{id: e.Dst, path: newPath})
			}
		}
		queue = nextQueue
	}

	return results, nil
}

// AddEdge creates an edge between two nodes in this namespace.
// If edge.Namespace is empty it is set to the namespace of this handle.
// If edge.ID is zero a new UUID is assigned.
func (h *NamespaceHandle) AddEdge(ctx context.Context, edge core.Edge) error {
	if edge.Namespace == "" {
		edge.Namespace = h.cfg.ID
	}
	if edge.ID == uuid.Nil {
		edge.ID = uuid.New()
	}
	if edge.TxTime.IsZero() {
		edge.TxTime = time.Now()
	}
	if edge.ValidFrom.IsZero() {
		edge.ValidFrom = time.Now()
	}
	return h.db.graph.UpsertEdge(ctx, edge)
}

// History returns all versions of a node, ordered oldest-first by
// transaction time.
func (h *NamespaceHandle) History(ctx context.Context, nodeID uuid.UUID) ([]core.Node, error) {
	return h.db.graph.History(ctx, h.cfg.ID, nodeID)
}
