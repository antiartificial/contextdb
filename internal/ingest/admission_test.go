package ingest_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/ingest"
)

func makeSource(cred float64, labels ...string) core.Source {
	return core.Source{
		ID: uuid.New(), Namespace: "test",
		ExternalID: uuid.New().String(),
		Alpha: cred * 10,
		Beta:  (1 - cred) * 10, Labels: labels,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func makeCandidate() core.Node {
	return core.Node{
		ID: uuid.New(), Namespace: "test",
		Labels: []string{"Claim"}, ValidFrom: time.Now(),
	}
}

func TestAdmit_ModeratorAlwaysAdmitted(t *testing.T) {
	is := is.New(t)
	d := ingest.Admit(ingest.AdmitRequest{
		Candidate:         makeCandidate(),
		Source:            makeSource(0.5, "moderator"),
		NearestNeighbours: []core.ScoredNode{{SimilarityScore: 0.50}},
		Threshold:         0.25,
	})
	is.True(d.Admit)
	is.Equal(1.0, d.ConfidenceMultiplier)
}

func TestAdmit_TrollAlwaysRejected(t *testing.T) {
	is := is.New(t)
	d := ingest.Admit(ingest.AdmitRequest{
		Candidate: makeCandidate(),
		Source:    makeSource(0.99, "troll"), // high stored score, label overrides
		Threshold: 0.01,
	})
	is.True(!d.Admit)
}

func TestAdmit_NearDuplicateRejected(t *testing.T) {
	is := is.New(t)
	d := ingest.Admit(ingest.AdmitRequest{
		Candidate:         makeCandidate(),
		Source:            makeSource(0.9),
		NearestNeighbours: []core.ScoredNode{{SimilarityScore: 0.97}},
		Threshold:         0.1,
	})
	is.True(!d.Admit)
}

// TestAdmit_PoisoningScenario is the headline test.
// 5 troll writes (high vector similarity) vs 1 trusted write.
// The trusted claim must score higher in retrieval.
func TestAdmit_PoisoningScenario(t *testing.T) {
	is := is.New(t)
	params := core.BeliefSystemParams()
	asOf := time.Now()
	params.AsOf = asOf

	trollSrc := makeSource(0.08)
	trollClaim := core.Node{
		ID: uuid.New(), Namespace: "channel:general",
		Labels:     []string{"Claim"},
		Confidence: trollSrc.EffectiveCredibility(), // 0.08
		ValidFrom:  asOf.Add(-time.Millisecond),
	}

	trustedSrc := makeSource(0.0, "moderator")
	trustedClaim := core.Node{
		ID: uuid.New(), Namespace: "channel:general",
		Labels:     []string{"Claim"},
		Confidence: trustedSrc.EffectiveCredibility(), // 1.0
		ValidFrom:  asOf.Add(-time.Millisecond),
	}

	// Trolls have higher similarity to the query — the poisoning advantage
	snTroll := core.ScoreNode(trollClaim, 0.92, 1.0, params)
	snTrusted := core.ScoreNode(trustedClaim, 0.65, 1.0, params)

	t.Logf("troll   score=%.4f (sim=0.92 conf=0.08)", snTroll.Score)
	t.Logf("trusted score=%.4f (sim=0.65 conf=1.00)", snTrusted.Score)
	t.Logf("delta trusted-troll = %+.4f", snTrusted.Score-snTroll.Score)

	is.True(snTrusted.Score > snTroll.Score)
}

func TestAdmit_CredibilityThresholdSweep(t *testing.T) {
	threshold := 0.25
	nearestSim := 0.5
	t.Log("credibility | novelty | combined | admitted")
	t.Log("------------|---------|----------|----------")
	for _, cred := range []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.7, 0.9, 1.0} {
		src := makeSource(cred)
		d := ingest.Admit(ingest.AdmitRequest{
			Candidate:         makeCandidate(),
			Source:            src,
			NearestNeighbours: []core.ScoredNode{{SimilarityScore: nearestSim}},
			Threshold:         threshold,
		})
		novelty := 1.0 - nearestSim
		combined := src.EffectiveCredibility() * novelty
		t.Logf("  %.2f       | %.2f    | %.4f   | %v", cred, novelty, combined, d.Admit)
	}
}
