package client

import (
	"context"
	"fmt"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	"github.com/google/uuid"
	"sort"
	"time"
)

// ExplainRankRequest compares two nodes under the namespace scoring strategy.
type ExplainRankRequest struct {
	NodeID      uuid.UUID
	OtherNodeID uuid.UUID
	Text        string
	Vector      []float32
	ScoreParams core.ScoreParams
	AsOf        time.Time
	MaxDepth    int
}

// RankedNodeExplanation is one side of a rank comparison.
type RankedNodeExplanation struct {
	NodeID          uuid.UUID           `json:"node_id"`
	Text            string              `json:"text,omitempty"`
	Score           float64             `json:"score"`
	SimilarityScore float64             `json:"similarity_score"`
	ConfidenceScore float64             `json:"confidence_score"`
	RecencyScore    float64             `json:"recency_score"`
	UtilityScore    float64             `json:"utility_score"`
	ScoreBreakdown  core.ScoreBreakdown `json:"score_breakdown"`
	RetrievalSource string              `json:"retrieval_source"`
	Evidence        RankEvidence        `json:"evidence"`
}

// RankEvidence summarizes graph evidence connected to one ranked node.
type RankEvidence struct {
	CompoundConfidence float64            `json:"compound_confidence"`
	SupportCount       int                `json:"support_count"`
	Links              []RankEvidenceLink `json:"links"`
}

// RankEvidenceLink is one supporting edge in a rank explanation.
type RankEvidenceLink struct {
	NodeID     uuid.UUID `json:"node_id"`
	EdgeID     uuid.UUID `json:"edge_id"`
	EdgeWeight float64   `json:"edge_weight"`
	Confidence float64   `json:"confidence"`
	Text       string    `json:"text,omitempty"`
}

// RankFactorDelta explains how much one score component favored NodeID.
type RankFactorDelta struct {
	Factor            string  `json:"factor"`
	NodeContribution  float64 `json:"node_contribution"`
	OtherContribution float64 `json:"other_contribution"`
	Delta             float64 `json:"delta"`
}

// RankExplanation explains why one node ranks above another.
type RankExplanation struct {
	Node         RankedNodeExplanation `json:"node"`
	Other        RankedNodeExplanation `json:"other"`
	WinnerNodeID uuid.UUID             `json:"winner_node_id,omitempty"`
	LoserNodeID  uuid.UUID             `json:"loser_node_id,omitempty"`
	Margin       float64               `json:"margin"`
	Summary      string                `json:"summary"`
	Factors      []RankFactorDelta     `json:"factors"`
}

// ExplainRank compares two nodes and returns the score factors that separate them.
func (h *NamespaceHandle) ExplainRank(ctx context.Context, req ExplainRankRequest) (*RankExplanation, error) {
	if req.NodeID == uuid.Nil || req.OtherNodeID == uuid.Nil {
		return nil, fmt.Errorf("explain rank: both node IDs are required")
	}
	if len(req.Vector) == 0 && req.Text != "" && h.db.opts.Embedder != nil {
		vecs, err := h.db.opts.Embedder.Embed(ctx, []string{req.Text})
		if err != nil {
			return nil, fmt.Errorf("explain rank: auto-embed query: %w", err)
		}
		if len(vecs) > 0 {
			req.Vector = vecs[0]
		}
	}

	left, err := h.db.graph.GetNode(ctx, h.cfg.ID, req.NodeID)
	if err != nil {
		return nil, fmt.Errorf("explain rank: get node: %w", err)
	}
	if left == nil {
		return nil, fmt.Errorf("explain rank: node %s not found", req.NodeID)
	}
	right, err := h.db.graph.GetNode(ctx, h.cfg.ID, req.OtherNodeID)
	if err != nil {
		return nil, fmt.Errorf("explain rank: get other node: %w", err)
	}
	if right == nil {
		return nil, fmt.Errorf("explain rank: node %s not found", req.OtherNodeID)
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

	leftScored := scoreRankNode(*left, req.Vector, params)
	rightScored := scoreRankNode(*right, req.Vector, params)
	leftEvidence, err := h.rankEvidence(ctx, leftScored.Node.ID, req.MaxDepth)
	if err != nil {
		return nil, err
	}
	rightEvidence, err := h.rankEvidence(ctx, rightScored.Node.ID, req.MaxDepth)
	if err != nil {
		return nil, err
	}
	leftExplanation := newRankedNodeExplanation(leftScored)
	leftExplanation.Evidence = leftEvidence
	rightExplanation := newRankedNodeExplanation(rightScored)
	rightExplanation.Evidence = rightEvidence
	explanation := &RankExplanation{
		Node:    leftExplanation,
		Other:   rightExplanation,
		Margin:  leftScored.Score - rightScored.Score,
		Factors: rankFactorDeltas(leftScored.Breakdown, rightScored.Breakdown),
	}
	switch {
	case leftScored.Score > rightScored.Score:
		explanation.WinnerNodeID = leftScored.Node.ID
		explanation.LoserNodeID = rightScored.Node.ID
		explanation.Summary = rankSummary(leftScored, rightScored)
	case rightScored.Score > leftScored.Score:
		explanation.WinnerNodeID = rightScored.Node.ID
		explanation.LoserNodeID = leftScored.Node.ID
		explanation.Summary = rankSummary(rightScored, leftScored)
	default:
		explanation.Summary = "The nodes are tied under the current scoring inputs."
	}
	return explanation, nil
}

func (h *NamespaceHandle) rankEvidence(ctx context.Context, nodeID uuid.UUID, maxDepth int) (RankEvidence, error) {
	chain, err := retrieval.TraceInferenceChain(ctx, h.db.graph, h.cfg.ID, nodeID, maxDepth)
	if err != nil {
		return RankEvidence{}, fmt.Errorf("explain rank: trace evidence: %w", err)
	}
	if chain == nil {
		return RankEvidence{}, nil
	}
	evidence := RankEvidence{
		CompoundConfidence: chain.CompoundConfidence,
		SupportCount:       len(chain.Links),
		Links:              make([]RankEvidenceLink, 0, len(chain.Links)),
	}
	for _, link := range chain.Links {
		evidence.Links = append(evidence.Links, RankEvidenceLink{
			NodeID:     link.Node.ID,
			EdgeID:     link.Edge.ID,
			EdgeWeight: link.Edge.Weight,
			Confidence: link.Confidence,
			Text:       core.NodeText(link.Node),
		})
	}
	return evidence, nil
}

func scoreRankNode(node core.Node, vector []float32, params core.ScoreParams) core.ScoredNode {
	similarity := 0.0
	source := "score"
	if len(vector) > 0 && len(node.Vector) > 0 {
		similarity = core.CosineSimilarity(vector, node.Vector) * 0.45
		source = "vector"
	}
	scored := core.ScoreNode(node, similarity, propertyFloat(node.Properties, "utility", 1.0), params)
	scored.RetrievalSource = source
	return scored
}

func newRankedNodeExplanation(scored core.ScoredNode) RankedNodeExplanation {
	return RankedNodeExplanation{
		NodeID:          scored.Node.ID,
		Text:            core.NodeText(scored.Node),
		Score:           scored.Score,
		SimilarityScore: scored.SimilarityScore,
		ConfidenceScore: scored.ConfidenceScore,
		RecencyScore:    scored.RecencyScore,
		UtilityScore:    scored.UtilityScore,
		ScoreBreakdown:  scored.Breakdown,
		RetrievalSource: scored.RetrievalSource,
	}
}

func rankFactorDeltas(left, right core.ScoreBreakdown) []RankFactorDelta {
	factors := []RankFactorDelta{
		{Factor: "similarity", NodeContribution: left.Similarity, OtherContribution: right.Similarity},
		{Factor: "confidence", NodeContribution: left.Confidence, OtherContribution: right.Confidence},
		{Factor: "recency", NodeContribution: left.Recency, OtherContribution: right.Recency},
		{Factor: "utility", NodeContribution: left.Utility, OtherContribution: right.Utility},
	}
	for i := range factors {
		factors[i].Delta = factors[i].NodeContribution - factors[i].OtherContribution
	}
	sort.SliceStable(factors, func(i, j int) bool {
		return absFloat(factors[i].Delta) > absFloat(factors[j].Delta)
	})
	return factors
}

func rankSummary(winner, loser core.ScoredNode) string {
	factors := rankFactorDeltas(winner.Breakdown, loser.Breakdown)
	if len(factors) == 0 || absFloat(factors[0].Delta) == 0 {
		return fmt.Sprintf("%s ranks above %s by %.4f points.", winner.Node.ID, loser.Node.ID, winner.Score-loser.Score)
	}
	return fmt.Sprintf("%s ranks above %s by %.4f points; %s contributes the largest difference.",
		winner.Node.ID, loser.Node.ID, winner.Score-loser.Score, factors[0].Factor)
}
