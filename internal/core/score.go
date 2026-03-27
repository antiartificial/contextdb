package core

import (
	"math"
	"time"
)

// ScoreParams holds caller-supplied weights and decay configuration.
// Zero values fall back to sensible defaults. This is the primary
// tuning surface — different namespaces supply different params.
type ScoreParams struct {
	// Weights must sum to 1.0 for interpretable scores, but are
	// normalised internally if they don't.
	SimilarityWeight float64 // how much vector similarity matters
	ConfidenceWeight float64 // how much node confidence matters
	RecencyWeight    float64 // how much freshness matters
	UtilityWeight    float64 // how much past usefulness matters (agent memory)

	// DecayAlpha overrides the memory-type default. Zero = use type default.
	DecayAlpha float64

	// AsOf is the temporal anchor for validity and age calculations.
	// Zero value defaults to time.Now().
	AsOf time.Time
}

func (p ScoreParams) withDefaults() ScoreParams {
	total := p.SimilarityWeight + p.ConfidenceWeight + p.RecencyWeight + p.UtilityWeight
	if total == 0 {
		p.SimilarityWeight = 0.40
		p.ConfidenceWeight = 0.30
		p.RecencyWeight = 0.20
		p.UtilityWeight = 0.10
	}
	if p.DecayAlpha == 0 {
		p.DecayAlpha = 0.05
	}
	if p.AsOf.IsZero() {
		p.AsOf = time.Now()
	}
	return p
}

// ScoredNode is a retrieval result with a full score breakdown.
// Callers can inspect components for debugging or visualisation.
type ScoredNode struct {
	Node

	// Composite
	Score float64

	// Components — each in [0, 1]
	SimilarityScore float64
	ConfidenceScore float64
	RecencyScore    float64
	UtilityScore    float64

	// Provenance
	RetrievalSource string // "vector", "graph", "kv", "fused"
}

// ScoreNode computes the composite retrieval score for a candidate node.
//
//   - similarity: vector cosine or graph proximity score [0, 1]
//   - utility:    agent-feedback score [0, 1]; pass 1.0 for belief-system nodes
//   - p:          caller-supplied weights and decay config
//
// Returns a ScoredNode with Score == 0 if the node is not valid at p.AsOf.
func ScoreNode(n Node, similarity, utility float64, p ScoreParams) ScoredNode {
	p = p.withDefaults()

	if !n.IsValidAt(p.AsOf) {
		return ScoredNode{Node: n, Score: 0, RetrievalSource: "invalid"}
	}

	// recency decay
	age := p.AsOf.Sub(n.ValidFrom).Hours()
	if age < 0 {
		age = 0
	}
	recency := math.Exp(-p.DecayAlpha * age)

	// confidence: treat 0 as neutral 0.5
	conf := n.Confidence
	if conf == 0 {
		conf = 0.5
	}

	// expiry-aware penalty: reduce confidence as ValidUntil approaches
	if n.ValidUntil != nil {
		hoursUntilExpiry := n.ValidUntil.Sub(p.AsOf).Hours()
		if hoursUntilExpiry > 0 {
			// Exponential penalty with β = 0.02 (noticeable within ~48h of expiry)
			conf *= clamp01(1.0 - math.Exp(-0.02*hoursUntilExpiry))
		}
	}

	// clamp inputs
	similarity = clamp01(similarity)
	utility = clamp01(utility)
	conf = clamp01(conf)
	recency = clamp01(recency)

	// normalise weights so they sum to 1.0
	total := p.SimilarityWeight + p.ConfidenceWeight + p.RecencyWeight + p.UtilityWeight
	if total == 0 {
		total = 1
	}
	sw := p.SimilarityWeight / total
	cw := p.ConfidenceWeight / total
	rw := p.RecencyWeight / total
	uw := p.UtilityWeight / total

	score := sw*similarity + cw*conf + rw*recency + uw*utility

	return ScoredNode{
		Node:            n,
		Score:           score,
		SimilarityScore: similarity,
		ConfidenceScore: conf,
		RecencyScore:    recency,
		UtilityScore:    utility,
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

// Preset strategy configurations for common namespace modes.

// BeliefSystemParams returns score params tuned for credibility-weighted
// belief systems (e.g. Channel bots, fact-tracking systems).
// Graph and confidence are weighted heavily; utility is unused.
func BeliefSystemParams() ScoreParams {
	return ScoreParams{
		SimilarityWeight: 0.30,
		ConfidenceWeight: 0.45,
		RecencyWeight:    0.20,
		UtilityWeight:    0.05,
		DecayAlpha:       0.03,
	}
}

// AgentMemoryParams returns score params tuned for agentic memory.
// Recency and utility (task outcome feedback) are weighted heavily.
func AgentMemoryParams() ScoreParams {
	return ScoreParams{
		SimilarityWeight: 0.35,
		ConfidenceWeight: 0.20,
		RecencyWeight:    0.25,
		UtilityWeight:    0.20,
		DecayAlpha:       0.05,
	}
}

// GeneralParams returns balanced params suitable for most use cases.
func GeneralParams() ScoreParams {
	return ScoreParams{
		SimilarityWeight: 0.40,
		ConfidenceWeight: 0.30,
		RecencyWeight:    0.20,
		UtilityWeight:    0.10,
		DecayAlpha:       0.05,
	}
}

// ProceduralParams returns params tuned for procedural/skill memories —
// very slow decay, confidence-forward.
func ProceduralParams() ScoreParams {
	return ScoreParams{
		SimilarityWeight: 0.40,
		ConfidenceWeight: 0.40,
		RecencyWeight:    0.15,
		UtilityWeight:    0.05,
		DecayAlpha:       DecayAlpha(MemoryProcedural),
	}
}
