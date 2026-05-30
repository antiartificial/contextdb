package core_test

import (
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
)

func freshNode(t time.Time, confidence float64) core.Node {
	return core.Node{
		ID:         uuid.New(),
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Confidence: confidence,
		ValidFrom:  t,
	}
}

func TestScoreNode_ZeroParamsGivesPositiveScore(t *testing.T) {
	is := is.New(t)
	n := freshNode(time.Now(), 0.8)
	sn := core.ScoreNode(n, 0.9, 1.0, core.ScoreParams{})
	is.True(sn.Score > 0)
	is.True(sn.Score <= 1.0)
}

func TestScoreNode_ExpiredNodeScoresZero(t *testing.T) {
	is := is.New(t)
	past := time.Now().Add(-48 * time.Hour)
	exp := time.Now().Add(-24 * time.Hour)
	n := core.Node{
		ID: uuid.New(), Namespace: "test", Confidence: 1.0,
		ValidFrom: past, ValidUntil: &exp,
	}
	sn := core.ScoreNode(n, 1.0, 1.0, core.ScoreParams{})
	is.Equal(0.0, sn.Score)
}

func TestScoreNode_FutureNodeScoresZero(t *testing.T) {
	is := is.New(t)
	n := core.Node{
		ID: uuid.New(), Namespace: "test", Confidence: 1.0,
		ValidFrom: time.Now().Add(1 * time.Hour),
	}
	sn := core.ScoreNode(n, 1.0, 1.0, core.ScoreParams{})
	is.Equal(0.0, sn.Score)
}

func TestScoreNode_RecencyDecay(t *testing.T) {
	is := is.New(t)
	p := core.GeneralParams()
	p.AsOf = time.Now()
	snNew := core.ScoreNode(freshNode(time.Now().Add(-1*time.Hour), 0.8), 0.7, 1.0, p)
	snOld := core.ScoreNode(freshNode(time.Now().Add(-72*time.Hour), 0.8), 0.7, 1.0, p)
	is.True(snNew.Score > snOld.Score)
}

// TestScoreNode_BeliefSystem_TrustedBeatsHighSimTroll is the core
// poisoning-resistance assertion for the belief-system preset.
// A moderately similar trusted claim must outscore a very similar troll claim.
func TestScoreNode_BeliefSystem_TrustedBeatsHighSimTroll(t *testing.T) {
	is := is.New(t)
	asOf := time.Now()
	p := core.BeliefSystemParams()
	p.AsOf = asOf

	trusted := freshNode(asOf.Add(-time.Millisecond), 0.95)
	snTrusted := core.ScoreNode(trusted, 0.60, 1.0, p)

	troll := freshNode(asOf.Add(-time.Millisecond), 0.05)
	snTroll := core.ScoreNode(troll, 0.90, 1.0, p)

	t.Logf("trusted score=%.4f (sim=0.60 conf=0.95)", snTrusted.Score)
	t.Logf("troll   score=%.4f (sim=0.90 conf=0.05)", snTroll.Score)
	is.True(snTrusted.Score > snTroll.Score)
}

func TestScoreNode_UtilityMattersForAgentMemory(t *testing.T) {
	is := is.New(t)
	asOf := time.Now()
	p := core.AgentMemoryParams()
	p.AsOf = asOf
	n := freshNode(asOf.Add(-time.Millisecond), 0.7)
	snHigh := core.ScoreNode(n, 0.6, 0.95, p)
	snLow := core.ScoreNode(n, 0.6, 0.10, p)
	t.Logf("high-utility=%.4f  low-utility=%.4f", snHigh.Score, snLow.Score)
	is.True(snHigh.Score > snLow.Score)
}

func TestScoreNode_ProceduralDecaysSlowerThanEpisodic(t *testing.T) {
	is := is.New(t)
	asOf := time.Now()
	created := asOf.Add(-48 * time.Hour)

	epParams := core.ScoreParams{
		SimilarityWeight: 0.4, ConfidenceWeight: 0.3,
		RecencyWeight: 0.2, UtilityWeight: 0.1,
		DecayAlpha: core.DecayAlpha(core.MemoryEpisodic), AsOf: asOf,
	}
	prParams := core.ProceduralParams()
	prParams.AsOf = asOf

	n := freshNode(created, 0.8)
	snEp := core.ScoreNode(n, 0.8, 1.0, epParams)
	snPr := core.ScoreNode(n, 0.8, 1.0, prParams)

	t.Logf("episodic recency=%.4f  procedural recency=%.4f", snEp.RecencyScore, snPr.RecencyScore)
	is.True(snPr.RecencyScore > snEp.RecencyScore)
}

func TestScoreNode_WeightNormalisationIsIdempotent(t *testing.T) {
	is := is.New(t)
	asOf := time.Now()
	n := freshNode(asOf, 0.7)

	norm := core.ScoreParams{
		SimilarityWeight: 0.4, ConfidenceWeight: 0.3,
		RecencyWeight: 0.2, UtilityWeight: 0.1, AsOf: asOf,
	}
	unnorm := core.ScoreParams{
		SimilarityWeight: 4.0, ConfidenceWeight: 3.0,
		RecencyWeight: 2.0, UtilityWeight: 1.0, AsOf: asOf,
	}
	snN := core.ScoreNode(n, 0.7, 0.8, norm)
	snU := core.ScoreNode(n, 0.7, 0.8, unnorm)

	diff := snN.Score - snU.Score
	if diff < 0 {
		diff = -diff
	}
	is.True(diff < 1e-9)
}

func TestScoreNode_BreakdownSumsToScore(t *testing.T) {
	is := is.New(t)
	asOf := time.Now()
	n := freshNode(asOf, 0.8)
	sn := core.ScoreNode(n, 0.75, 0.5, core.ScoreParams{
		SimilarityWeight: 4,
		ConfidenceWeight: 3,
		RecencyWeight:    2,
		UtilityWeight:    1,
		AsOf:             asOf,
	})

	sum := sn.Breakdown.Similarity + sn.Breakdown.Confidence + sn.Breakdown.Recency + sn.Breakdown.Utility
	is.True(abs64(sum-sn.Score) < 1e-9)
	is.True(abs64(sn.Breakdown.Similarity-0.3) < 1e-9)
	is.True(abs64(sn.Breakdown.Confidence-0.24) < 1e-9)
	is.True(abs64(sn.Breakdown.Utility-0.05) < 1e-9)
}

func TestCosineSimilarity(t *testing.T) {
	is := is.New(t)

	is.True(abs64(core.CosineSimilarity([]float32{1, 0}, []float32{1, 0})-1.0) < 1e-6)
	is.True(abs64(core.CosineSimilarity([]float32{1, 0}, []float32{0, 1})-0.0) < 1e-6)
	is.True(abs64(core.CosineSimilarity([]float32{1, 0}, []float32{-1, 0})+1.0) < 1e-6)
	is.Equal(0.0, core.CosineSimilarity([]float32{1, 2, 3}, []float32{1, 2})) // mismatch
}

func TestPresetParams_AllWeightsPositive(t *testing.T) {
	is := is.New(t)
	for _, p := range []core.ScoreParams{
		core.BeliefSystemParams(),
		core.AgentMemoryParams(),
		core.GeneralParams(),
		core.ProceduralParams(),
	} {
		is.True(p.SimilarityWeight > 0)
		is.True(p.ConfidenceWeight > 0)
		is.True(p.RecencyWeight > 0)
		is.True(p.UtilityWeight > 0)
		is.True(p.DecayAlpha > 0)
	}
}

func TestScoreNode_ProvenanceAttenuation(t *testing.T) {
	now := time.Now()
	n := core.Node{
		ID: uuid.New(), ValidFrom: now, Confidence: 0.9,
	}

	// Direct claim (depth 0)
	direct := core.ScoreNode(n, 0.8, 1.0, core.ScoreParams{
		SimilarityWeight: 0.4, ConfidenceWeight: 0.6,
	})

	// Derived claim (depth 3)
	derived := core.ScoreNode(n, 0.8, 1.0, core.ScoreParams{
		SimilarityWeight: 0.4, ConfidenceWeight: 0.6,
		ProvenanceDepth: 3,
	})

	if derived.Score >= direct.Score {
		t.Errorf("derived (%v) should score lower than direct (%v)", derived.Score, direct.Score)
	}
}

func TestScoreNode_RankingGoldenByNamespaceMode(t *testing.T) {
	asOf := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	candidates := []goldenCandidate{
		{
			name:       "trusted-moderate-match",
			similarity: 0.72,
			utility:    0.30,
			node:       freshNode(asOf.Add(-12*time.Hour), 0.95),
		},
		{
			name:       "poison-near-match",
			similarity: 0.98,
			utility:    0.30,
			node:       freshNode(asOf.Add(-5*time.Minute), 0.05),
		},
		{
			name:       "useful-memory",
			similarity: 0.68,
			utility:    1.00,
			node:       freshNode(asOf.Add(-3*time.Hour), 0.65),
		},
		{
			name:       "fresh-balanced",
			similarity: 0.74,
			utility:    0.40,
			node:       freshNode(asOf.Add(-5*time.Minute), 0.60),
		},
		{
			name:       "old-established",
			similarity: 0.80,
			utility:    0.80,
			node:       freshNode(asOf.Add(-240*time.Hour), 0.90),
		},
	}

	tests := []struct {
		name string
		p    core.ScoreParams
		want []string
	}{
		{
			name: "belief system ranks trust over near-match poison",
			p:    core.BeliefSystemParams(),
			want: []string{
				"trusted-moderate-match",
				"useful-memory",
				"fresh-balanced",
				"old-established",
				"poison-near-match",
			},
		},
		{
			name: "agent memory ranks utility and freshness first",
			p:    core.AgentMemoryParams(),
			want: []string{
				"useful-memory",
				"fresh-balanced",
				"poison-near-match",
				"trusted-moderate-match",
				"old-established",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.p.AsOf = asOf
			got := rankGoldenCandidates(candidates, tt.p)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d ranked candidates, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i].name != tt.want[i] {
					t.Fatalf("rank %d = %q (score %.6f), want %q; full order=%v",
						i+1, got[i].name, got[i].score, tt.want[i], goldenNames(got))
				}
			}
		})
	}
}

type goldenCandidate struct {
	name       string
	node       core.Node
	similarity float64
	utility    float64
	score      float64
}

func rankGoldenCandidates(candidates []goldenCandidate, p core.ScoreParams) []goldenCandidate {
	out := append([]goldenCandidate(nil), candidates...)
	for i := range out {
		out[i].score = core.ScoreNode(out[i].node, out[i].similarity, out[i].utility, p).Score
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].score > out[j].score
	})
	return out
}

func goldenNames(candidates []goldenCandidate) []string {
	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = c.name
	}
	return names
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
