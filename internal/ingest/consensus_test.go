package ingest

import (
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/google/uuid"
	"github.com/matryer/is"
)

func TestMultiSourceConsensus_Empty(t *testing.T) {
	is := is.New(t)

	result := MultiSourceConsensus(nil)
	is.Equal(result.Probability, 0.5)
	is.Equal(result.Confidence, 0.0)
	is.Equal(result.Method, "none")
}

func TestMultiSourceConsensus_UnanimousSupport(t *testing.T) {
	is := is.New(t)

	claims := []ClaimAssertion{
		{SourceCredibility: 0.8, AssertionType: "supports"},
		{SourceCredibility: 0.9, AssertionType: "supports"},
		{SourceCredibility: 0.7, AssertionType: "supports"},
	}

	result := MultiSourceConsensus(claims)
	is.True(result.Probability > 0.9)
	is.True(result.Confidence > 0.5)
	is.Equal(result.SourceCount, 3)
	is.Equal(result.Method, "weighted")
}

func TestMultiSourceConsensus_UnanimousContradict(t *testing.T) {
	is := is.New(t)

	claims := []ClaimAssertion{
		{SourceCredibility: 0.8, AssertionType: "contradicts"},
		{SourceCredibility: 0.9, AssertionType: "contradicts"},
		{SourceCredibility: 0.7, AssertionType: "contradicts"},
	}

	result := MultiSourceConsensus(claims)
	is.True(result.Probability < 0.1)
	is.True(result.Confidence > 0.5)
}

func TestMultiSourceConsensus_Mixed(t *testing.T) {
	is := is.New(t)

	// Two high-credibility sources support, one low-cred contradicts
	claims := []ClaimAssertion{
		{SourceCredibility: 0.9, AssertionType: "supports"},
		{SourceCredibility: 0.85, AssertionType: "supports"},
		{SourceCredibility: 0.3, AssertionType: "contradicts"},
	}

	result := MultiSourceConsensus(claims)
	// High credibility sources should outweigh low credibility contradiction
	is.True(result.Probability > 0.6)
	// Lower confidence due to disagreement
	is.True(result.Confidence < 1.0)
}

func TestMultiSourceConsensus_DisagreementLowConfidence(t *testing.T) {
	is := is.New(t)

	// Equal high-credibility disagreement
	claims := []ClaimAssertion{
		{SourceCredibility: 0.9, AssertionType: "supports"},
		{SourceCredibility: 0.9, AssertionType: "contradicts"},
	}

	result := MultiSourceConsensus(claims)
	// Probability should be near 0.5
	is.True(result.Probability > 0.4 && result.Probability < 0.6)
	// Low confidence due to high disagreement
	is.True(result.Confidence < 0.5)
}

func TestClaimAssertion_VoteValue(t *testing.T) {
	is := is.New(t)

	support := ClaimAssertion{AssertionType: "supports"}
	is.Equal(support.VoteValue(), 1.0)

	contradict := ClaimAssertion{AssertionType: "contradicts"}
	is.Equal(contradict.VoteValue(), 0.0)

	abstain := ClaimAssertion{AssertionType: "abstains"}
	is.Equal(abstain.VoteValue(), 0.5)

	unknown := ClaimAssertion{AssertionType: "unknown"}
	is.Equal(unknown.VoteValue(), 0.5)
}

func TestRankSourcesByCredibility(t *testing.T) {
	is := is.New(t)

	sources := []core.Source{
		{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Alpha: 10, Beta: 2, ClaimsAsserted: 12},   // High credibility, many observations
		{ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Alpha: 5, Beta: 5, ClaimsAsserted: 10},    // Medium credibility, many observations
		{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Alpha: 10, Beta: 1, ClaimsAsserted: 2},    // Very high credibility, few observations (uncertain)
	}

	rankings := RankSourcesByCredibility(sources, 1.0)

	// Should have 3 rankings
	is.Equal(len(rankings), 3)

	// All rankings should have UCB >= mean credibility
	for _, r := range rankings {
		is.True(r.UCBScore >= r.Credibility)
	}

	// The high-credibility, high-observation source should rank well
	// The very high cred but low observation source might rank lower due to UCB
	// (UCB gives bonus to less-observed sources, but 10/11 is very high credibility)
}

func TestRankSourcesByCredibility_ExplorationBonus(t *testing.T) {
	is := is.New(t)

	// Source with few observations but decent credibility should get exploration bonus
	fewObs := core.Source{ID: uuid.New(), Alpha: 3, Beta: 1, ClaimsAsserted: 4}
	manyObs := core.Source{ID: uuid.New(), Alpha: 30, Beta: 10, ClaimsAsserted: 40}

	// Both have ~75% credibility, but fewObs gets exploration bonus
	sources := []core.Source{fewObs, manyObs}
	rankings := RankSourcesByCredibility(sources, 1.0)

	// Both should be ranked
	is.Equal(len(rankings), 2)

	// Check that exploration bonus is applied
	fewObsRank := rankings[0]
	manyObsRank := rankings[1]

	if rankings[0].SourceID != fewObs.ID {
		fewObsRank = rankings[1]
		manyObsRank = rankings[0]
	}

	// The source with fewer observations should have larger exploration bonus
	fewObsBonus := fewObsRank.UCBScore - fewObsRank.Credibility
	manyObsBonus := manyObsRank.UCBScore - manyObsRank.Credibility
	is.True(fewObsBonus > manyObsBonus)
}

func TestAnomalyDetector_CredibilityDrop(t *testing.T) {
	is := is.New(t)

	detector := AnomalyDetector{
		MinCredibilityDrop: 0.3,
	}

	sourceID := uuid.New()
	source := core.Source{
		ID:    sourceID,
		Alpha: 5,
		Beta:  5, // 50% credibility
	}

	history := map[uuid.UUID][]CredibilitySnapshot{
		sourceID: {
			{Timestamp: time.Now().Add(-48 * time.Hour), Credibility: 0.9, Alpha: 20, Beta: 2},
			{Timestamp: time.Now().Add(-24 * time.Hour), Credibility: 0.8, Alpha: 15, Beta: 3},
			{Timestamp: time.Now(), Credibility: 0.5, Alpha: 5, Beta: 5},
		},
	}

	anomalies := detector.DetectAnomalies([]core.Source{source}, history)

	// Should detect the credibility drop
	is.Equal(len(anomalies), 1)
	is.Equal(anomalies[0].Type, "credibility_drop")
	is.Equal(anomalies[0].SourceID, sourceID)
	is.True(anomalies[0].Severity > 0)
}

func TestAnomalyDetector_NoDropBelowThreshold(t *testing.T) {
	is := is.New(t)

	detector := AnomalyDetector{
		MinCredibilityDrop: 0.5, // High threshold
	}

	sourceID := uuid.New()
	source := core.Source{ID: sourceID}

	history := map[uuid.UUID][]CredibilitySnapshot{
		sourceID: {
			{Timestamp: time.Now().Add(-48 * time.Hour), Credibility: 0.9, Alpha: 20, Beta: 2},
			{Timestamp: time.Now(), Credibility: 0.7, Alpha: 10, Beta: 4},
		},
	}

	anomalies := detector.DetectAnomalies([]core.Source{source}, history)

	// No anomaly detected - drop is only 0.2, below threshold
	is.Equal(len(anomalies), 0)
}

func TestAnomalyDetector_ActivityBurst(t *testing.T) {
	is := is.New(t)

	detector := AnomalyDetector{}

	sourceID := uuid.New()
	source := core.Source{ID: sourceID}

	// Simulate activity burst: normal rate then sudden spike
	history := map[uuid.UUID][]CredibilitySnapshot{
		sourceID: {
			{Timestamp: time.Now().Add(-72 * time.Hour), Credibility: 0.5, Alpha: 2, Beta: 2},
			{Timestamp: time.Now().Add(-48 * time.Hour), Credibility: 0.5, Alpha: 4, Beta: 2}, // +2 claims
			{Timestamp: time.Now().Add(-24 * time.Hour), Credibility: 0.5, Alpha: 6, Beta: 2}, // +2 claims
			{Timestamp: time.Now(), Credibility: 0.5, Alpha: 26, Beta: 2},                   // +20 claims (burst!)
		},
	}

	anomalies := detector.DetectAnomalies([]core.Source{source}, history)

	// Should detect the activity burst
	foundBurst := false
	for _, a := range anomalies {
		if a.Type == "burst_activity" {
			foundBurst = true
			is.True(a.Severity > 0)
			break
		}
	}
	is.True(foundBurst)
}

func TestTruthEstimate_ValidRange(t *testing.T) {
	is := is.New(t)

	// Test that truth estimate is always in valid range
	testCases := [][]ClaimAssertion{
		{}, // Empty
		{{SourceCredibility: 1.0, AssertionType: "supports"}},
		{{SourceCredibility: 1.0, AssertionType: "contradicts"}},
		{
			{SourceCredibility: 0.5, AssertionType: "supports"},
			{SourceCredibility: 0.5, AssertionType: "contradicts"},
		},
	}

	for _, claims := range testCases {
		result := MultiSourceConsensus(claims)
		is.True(result.Probability >= 0 && result.Probability <= 1)
		is.True(result.Confidence >= 0 && result.Confidence <= 1)
	}
}

func TestConsensus_WithSourceVariance(t *testing.T) {
	is := is.New(t)

	// High credibility source with high variance (uncertain credibility)
	uncertainHigh := ClaimAssertion{
		SourceCredibility: 0.9,
		SourceVariance:    0.2, // High variance
		AssertionType:     "supports",
	}

	// Lower credibility source with low variance (more certain)
	certainLow := ClaimAssertion{
		SourceCredibility: 0.6,
		SourceVariance:    0.01, // Low variance
		AssertionType:     "contradicts",
	}

	// Both have equal weight, so result should be mixed
	claims := []ClaimAssertion{uncertainHigh, certainLow}
	result := MultiSourceConsensus(claims)

	// Result should be somewhere in between
	is.True(result.Probability > 0.3 && result.Probability < 0.8)
	// Lower confidence due to disagreement
	is.True(result.Confidence < 1.0)
}

func TestCredibilitySnapshot_DeltaCalculation(t *testing.T) {
	is := is.New(t)

	// Test delta calculation for activity burst detection
	snapshots := []CredibilitySnapshot{
		{Timestamp: time.Now().Add(-2 * time.Hour), Alpha: 10, Beta: 5},  // 15 total
		{Timestamp: time.Now().Add(-1 * time.Hour), Alpha: 12, Beta: 5},  // 17 total, +2
		{Timestamp: time.Now(), Alpha: 30, Beta: 5},                      // 35 total, +18
	}

	// Calculate deltas
	var deltas []float64
	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1].Alpha + snapshots[i-1].Beta
		curr := snapshots[i].Alpha + snapshots[i].Beta
		deltas = append(deltas, curr-prev)
	}

	is.Equal(len(deltas), 2)
	is.Equal(deltas[0], 2.0)
	is.Equal(deltas[1], 18.0)
	is.True(deltas[1] > deltas[0]*3) // 18 > 2*3, qualifies as burst
}
