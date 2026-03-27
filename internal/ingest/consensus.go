package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// TruthEstimate represents the inferred truth of a claim based on source consensus.
type TruthEstimate struct {
	ClaimID      uuid.UUID
	Probability  float64 // P(claim is true) in [0, 1]
	Confidence   float64 // uncertainty in the estimate
	SourceCount  int     // number of sources considered
	Method       string  // "majority", "weighted", "em"
}

// MultiSourceConsensus performs truth inference across multiple sources asserting
// the same or conflicting claims. Uses credibility-weighted voting.
//
// The algorithm:
// 1. Collect all sources that have asserted claims in the same topic/label space
// 2. Weight each source by its credibility (mean of Beta distribution)
// 3. Compute weighted vote: P(true) = sum(credible_i * vote_i) / sum(credible_i)
// 4. Confidence inversely proportional to source disagreement
func MultiSourceConsensus(claims []ClaimAssertion) TruthEstimate {
	if len(claims) == 0 {
		return TruthEstimate{Probability: 0.5, Confidence: 0, Method: "none"}
	}

	var weightedSum, totalWeight, squaredDiff float64

	for _, ca := range claims {
		// vote: 1 for assertion, 0 for contradiction, 0.5 for abstain/uncertain
		vote := ca.VoteValue()
		weight := ca.SourceCredibility

		// Observations carry less weight than assertions
		epistemicMultiplier := 1.0
		if ca.EpistemicType == core.EpistemicObservation {
			epistemicMultiplier = 0.6
		} else if ca.EpistemicType == core.EpistemicInference {
			epistemicMultiplier = 0.8
		}
		weight *= epistemicMultiplier

		weightedSum += weight * vote
		totalWeight += weight
	}

	if totalWeight == 0 {
		return TruthEstimate{Probability: 0.5, Confidence: 0, Method: "weighted"}
	}

	probability := weightedSum / totalWeight

	// Compute confidence based on agreement (lower variance = higher confidence)
	for _, ca := range claims {
		diff := ca.VoteValue() - probability
		squaredDiff += ca.SourceCredibility * diff * diff
	}

	variance := squaredDiff / totalWeight
	confidence := 1.0 - math.Min(variance*4, 1.0) // Scale to [0, 1]

	return TruthEstimate{
		Probability: probability,
		Confidence:  confidence,
		SourceCount: len(claims),
		Method:      "weighted",
	}
}

// ClaimAssertion represents a source's position on a claim.
type ClaimAssertion struct {
	ClaimID            uuid.UUID
	SourceID           uuid.UUID
	SourceCredibility  float64 // mean of Beta distribution (optionally domain-scoped)
	SourceVariance     float64 // uncertainty in credibility
	AssertionType      string  // "supports", "contradicts", "abstains"
	EpistemicType      string  // mirrors core.Node.EpistemicType
	Domain             string  // optional domain scope for credibility lookup
	Timestamp          time.Time
}

// VoteValue returns numeric value for voting calculation.
func (ca ClaimAssertion) VoteValue() float64 {
	switch ca.AssertionType {
	case "supports":
		return 1.0
	case "contradicts":
		return 0.0
	default:
		return 0.5
	}
}

// ConsensusResolver handles multi-source truth inference and credibility feedback.
type ConsensusResolver struct {
	graph     store.GraphStore
	logger    *slog.Logger
	Namespace string // namespace used for claim lookups; defaults to "default" when empty
}

// NewConsensusResolver creates a resolver for truth inference.
func NewConsensusResolver(graph store.GraphStore, logger *slog.Logger) *ConsensusResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConsensusResolver{graph: graph, logger: logger}
}

// ResolveTruth estimates the truth of a claim using all related assertions.
func (r *ConsensusResolver) ResolveTruth(ctx context.Context, claimID uuid.UUID) (TruthEstimate, error) {
	// Find all sources that have asserted this claim
	assertions, err := r.collectAssertions(ctx, claimID)
	if err != nil {
		return TruthEstimate{}, fmt.Errorf("collect assertions: %w", err)
	}

	if len(assertions) == 0 {
		return TruthEstimate{
			ClaimID:     claimID,
			Probability: 0.5,
			Confidence:  0,
			Method:      "none",
		}, nil
	}

	// Run weighted consensus
	estimate := MultiSourceConsensus(assertions)
	estimate.ClaimID = claimID

	return estimate, nil
}

// collectAssertions gathers all source assertions for a claim.
func (r *ConsensusResolver) collectAssertions(ctx context.Context, claimID uuid.UUID) ([]ClaimAssertion, error) {
	ns := r.Namespace
	if ns == "" {
		ns = "default"
	}

	// Find edges where this claim is the target.
	edges, err := r.graph.GetEdgesTo(ctx, ns, claimID)
	if err != nil {
		return nil, err
	}

	// Attempt to read the domain from the target claim node so credibility
	// lookups can be scoped to the relevant domain.
	var claimDomain string
	if claimNode, err := r.graph.GetNode(ctx, ns, claimID); err == nil && claimNode != nil {
		claimDomain, _ = claimNode.Properties["domain"].(string)
		// If no explicit domain property is set, derive it from the first label.
		// This ensures domain-scoped credibility is used even when the domain
		// property is absent, as long as the node carries at least one label.
		if claimDomain == "" && len(claimNode.Labels) > 0 {
			claimDomain = claimNode.Labels[0]
		}
	}

	var assertions []ClaimAssertion

	for _, edge := range edges {
		// Skip non-assertion edges
		if edge.Type != "asserts" && edge.Type != "contradicts" {
			continue
		}

		// Try to find the source node associated with the edge source
		srcNode, err := r.graph.GetNode(ctx, edge.Namespace, edge.Src)
		if err != nil || srcNode == nil {
			continue // skip sources we can't resolve
		}

		// Default credibility if source lookup fails
		var sourceCred float64 = 0.5
		var sourceVariance float64 = 0.25

		// The edge source should be a node that was created by a source.
		// Try to look up the source using the node's source information from properties.
		if srcID, ok := srcNode.Properties["source_id"].(string); ok && srcID != "" {
			if srcObj, err := r.graph.GetSourceByExternalID(ctx, edge.Namespace, srcID); err == nil && srcObj != nil {
				// Use domain-scoped credibility when a domain is known; falls back to global.
				sourceCred = srcObj.DomainCredibility(claimDomain)
				sourceVariance = srcObj.CredibilityVariance()
			}
		}

		assertionType := "supports"
		if edge.Type == "contradicts" {
			assertionType = "contradicts"
		}

		assertions = append(assertions, ClaimAssertion{
			ClaimID:           claimID,
			SourceID:          edge.Src,
			SourceCredibility: sourceCred,
			SourceVariance:    sourceVariance,
			AssertionType:     assertionType,
			Domain:            claimDomain,
			Timestamp:         edge.TxTime,
		})
	}

	return assertions, nil
}

// PropagateCredibilityFeedback applies consensus results back to source credibility.
// When a claim is determined to be true/false, sources that supported the correct
// outcome get credibility boosts; those who contradicted it get penalties.
func (r *ConsensusResolver) PropagateCredibilityFeedback(ctx context.Context, claimID uuid.UUID, truth TruthEstimate) error {
	if truth.Confidence < 0.7 {
		// Not confident enough to propagate feedback
		return nil
	}

	assertions, err := r.collectAssertions(ctx, claimID)
	if err != nil {
		return err
	}

	for _, assertion := range assertions {
		// Determine if source was correct
		vote := assertion.VoteValue()
		truthProb := truth.Probability

		// Source is "correct" if they voted in the direction of truth
		// High truth probability (>0.5) means "supports" was correct
		// Low truth probability (<0.5) means "contradicts" was correct
		validated := (truthProb > 0.5 && vote > 0.5) || (truthProb < 0.5 && vote < 0.5)

		// Weight the update by how confident we are in the truth estimate
		weight := truth.Confidence

		// Get the source node to find its associated source record
		srcNode, err := r.graph.GetNode(ctx, "", assertion.SourceID)
		if err != nil {
			continue
		}

		// Lookup source by external ID from node properties
		srcID, ok := srcNode.Properties["source_id"].(string)
		if !ok || srcID == "" {
			continue
		}

		src, err := r.graph.GetSourceByExternalID(ctx, srcNode.Namespace, srcID)
		if err != nil {
			continue
		}

		src.WeightedBayesianUpdate(validated, weight)

		// Persist updated source
		if err := r.graph.UpsertSource(ctx, *src); err != nil {
			r.logger.Error("failed to update source credibility", "source", src.ID, "error", err)
		}
	}

	return nil
}

// SourceRanking ranks sources by their credibility with uncertainty handling.
// Uses upper confidence bound (UCB) algorithm for exploration-exploitation.
type SourceRanking struct {
	SourceID    uuid.UUID
	Credibility float64
	UCBScore    float64 // upper confidence bound for ranking
}

// RankSourcesByCredibility returns sources sorted by UCB score.
// The UCB formula: mean + sqrt(2 * ln(total_observations) / observations)
// This balances exploitation (high credibility) with exploration (uncertain sources)
func RankSourcesByCredibility(sources []core.Source, explorationParam float64) []SourceRanking {
	if explorationParam <= 0 {
		explorationParam = 1.0
	}

	// Total observations across all sources
	var totalObs int64
	for _, s := range sources {
		totalObs += s.ClaimsAsserted
	}
	if totalObs == 0 {
		totalObs = 1
	}
	lnTotal := math.Log(float64(totalObs))

	rankings := make([]SourceRanking, len(sources))
	for i, s := range sources {
		mean := s.EffectiveCredibility()
		n := float64(s.ClaimsAsserted)
		if n < 1 {
			n = 1
		}

		// Upper confidence bound
		ucb := mean + explorationParam*math.Sqrt(2*lnTotal/n)

		rankings[i] = SourceRanking{
			SourceID:    s.ID,
			Credibility: mean,
			UCBScore:    ucb,
		}
	}

	// Sort by UCB score descending
	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].UCBScore > rankings[j].UCBScore
	})

	return rankings
}

// AnomalyDetector identifies sources with unusual behavior patterns.
type AnomalyDetector struct {
	// Thresholds for anomaly detection
	MinCredibilityDrop float64 // e.g., 0.3 (30% drop)
	MinTimeWindow      time.Duration
}

// SourceAnomaly represents a detected anomaly in source behavior.
type SourceAnomaly struct {
	SourceID       uuid.UUID
	Type           string  // "credibility_drop", "burst_activity", "contradiction_spike"
	Severity       float64 // 0-1
	Details        string
	RecommendedAction string
}

// DetectAnomalies scans sources for unusual patterns.
func (ad *AnomalyDetector) DetectAnomalies(sources []core.Source, history map[uuid.UUID][]CredibilitySnapshot) []SourceAnomaly {
	var anomalies []SourceAnomaly

	for _, s := range sources {
		snapshots := history[s.ID]
		if len(snapshots) < 2 {
			continue
		}

		// Check for credibility drop
		if anomaly := ad.checkCredibilityDrop(s, snapshots); anomaly != nil {
			anomalies = append(anomalies, *anomaly)
		}

		// Check for activity burst
		if anomaly := ad.checkActivityBurst(s, snapshots); anomaly != nil {
			anomalies = append(anomalies, *anomaly)
		}
	}

	return anomalies
}

// CredibilitySnapshot captures source state at a point in time.
type CredibilitySnapshot struct {
	Timestamp   time.Time
	Credibility float64
	Alpha       float64
	Beta        float64
}

func (ad *AnomalyDetector) checkCredibilityDrop(s core.Source, history []CredibilitySnapshot) *SourceAnomaly {
	if len(history) < 2 {
		return nil
	}

	// Compare recent credibility to historical average
	recent := history[len(history)-1]
	var sum float64
	for _, snap := range history[:len(history)-1] {
		sum += snap.Credibility
	}
	historicalAvg := sum / float64(len(history)-1)

	drop := historicalAvg - recent.Credibility
	if ad.MinCredibilityDrop > 0 && drop >= ad.MinCredibilityDrop {
		return &SourceAnomaly{
			SourceID:          s.ID,
			Type:              "credibility_drop",
			Severity:          math.Min(drop*2, 1.0), // Scale: 0.5 drop = severity 1.0
			Details:           fmt.Sprintf("Credibility dropped %.2f (from %.2f to %.2f)", drop, historicalAvg, recent.Credibility),
			RecommendedAction: "review_recent_claims",
		}
	}

	return nil
}

func (ad *AnomalyDetector) checkActivityBurst(s core.Source, history []CredibilitySnapshot) *SourceAnomaly {
	if len(history) < 3 {
		return nil
	}

	// Compare recent activity to baseline
	recentWindow := history[len(history)-2:]
	baselineWindow := history[:len(history)-2]

	var recentClaims, baselineClaims float64
	for i := 1; i < len(recentWindow); i++ {
		recentClaims += recentWindow[i].Alpha + recentWindow[i].Beta -
			recentWindow[i-1].Alpha - recentWindow[i-1].Beta
	}
	for i := 1; i < len(baselineWindow); i++ {
		baselineClaims += baselineWindow[i].Alpha + baselineWindow[i].Beta -
			baselineWindow[i-1].Alpha - baselineWindow[i-1].Beta
	}

	baselineRate := baselineClaims / float64(len(baselineWindow)-1)
	recentRate := recentClaims / float64(len(recentWindow)-1)

	if baselineRate > 0 && recentRate > baselineRate*3 {
		return &SourceAnomaly{
			SourceID:          s.ID,
			Type:              "burst_activity",
			Severity:          math.Min(recentRate/baselineRate/5, 1.0),
			Details:           fmt.Sprintf("Activity increased %.1fx above baseline", recentRate/baselineRate),
			RecommendedAction: "verify_claim_quality",
		}
	}

	return nil
}
