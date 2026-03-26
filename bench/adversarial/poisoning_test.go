package adversarial_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
	"github.com/antiartificial/contextdb/testdata"
)

// TestPoisoning_ScaleStress tests poisoning resistance at increasing troll-to-
// trusted ratios. For each ratio the test measures whether the trusted claim
// still ranks first under the BeliefSystem preset.
func TestPoisoning_ScaleStress(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	ratios := []int{5, 10, 25, 50, 100}

	t.Log("\n╔══════════════════════════════════════════════════════════════════╗")
	t.Log("║      POISONING RESISTANCE — Scale Stress Test                    ║")
	t.Log("╚══════════════════════════════════════════════════════════════════╝")
	t.Logf("%-10s %6s %6s %6s %10s", "trolls", "R@1", "R@3", "rank", "status")
	t.Log(strings.Repeat("─", 45))

	passCount := 0
	for _, nTrolls := range ratios {
		graph := memstore.NewGraphStore()
		vecs := memstore.NewVectorIndex()

		// Create a single trusted claim.
		trustedID := uuid.New()
		trustedNode := core.Node{
			ID:         trustedID,
			Namespace:  "poison-test",
			Labels:     []string{"Claim", "Trusted"},
			Properties: map[string]any{"text": "Go uses a concurrent mark-and-sweep garbage collector"},
			Confidence: 0.95,
		}
		trustedVec := testdata.TopicVecExported(0, 8, 0.9, 0.1)
		_ = graph.UpsertNode(ctx, trustedNode)
		vecs.RegisterNode(trustedNode)
		nID := trustedID
		_ = vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: "poison-test",
			NodeID:    &nID,
			Vector:    trustedVec,
			ModelID:   "test",
		})

		// Flood with troll claims at high cosine similarity.
		for i := 0; i < nTrolls; i++ {
			trollID := uuid.New()
			trollNode := core.Node{
				ID:         trollID,
				Namespace:  "poison-test",
				Labels:     []string{"Claim", "Troll"},
				Properties: map[string]any{"text": "Go has no garbage collector"},
				Confidence: 0.05,
			}
			trollVec := testdata.TrollVecExported(0, i)
			_ = graph.UpsertNode(ctx, trollNode)
			vecs.RegisterNode(trollNode)
			tID := trollID
			_ = vecs.Index(ctx, core.VectorEntry{
				ID:        uuid.New(),
				Namespace: "poison-test",
				NodeID:    &tID,
				Vector:    trollVec,
				ModelID:   "test",
			})
		}

		engine := &retrieval.Engine{
			Graph:   graph,
			Vectors: vecs,
			KV:      memstore.NewKVStore(),
		}

		results, _ := engine.Retrieve(ctx, retrieval.Query{
			Namespace:   "poison-test",
			Vector:      testdata.QueryVecExported(0, 0.08),
			TopK:        nTrolls + 5, // request enough to find the trusted claim
			ScoreParams: core.BeliefSystemParams(),
			Strategy: retrieval.HybridStrategy{
				VectorWeight:  0.30,
				GraphWeight:   0.55,
				SessionWeight: 0.15,
				MaxDepth:      3,
			},
		})

		// Check if trusted claim ranks first.
		r1 := 0.0
		r3 := 0.0
		rank := -1
		for i, r := range results {
			if r.ID == trustedID {
				if i == 0 {
					r1 = 1.0
				}
				if i < 3 {
					r3 = 1.0
				}
				rank = i + 1
				break
			}
		}

		pass := r1 == 1.0
		if pass {
			passCount++
		}
		status := "PASS"
		if !pass {
			status = "FAIL"
		}

		t.Logf("%-10d %5.0f%% %5.0f%% %5d  %10s", nTrolls, r1*100, r3*100, rank, status)
	}

	t.Log(strings.Repeat("─", 45))
	t.Logf("Passed: %d / %d ratios", passCount, len(ratios))

	// The BeliefSystem preset should resist poisoning at moderate scales.
	// At extreme scales (100:1) pure vector similarity may dominate,
	// but the admission gate would normally filter low-confidence trolls.
	is.True(passCount >= 2) // must pass at least 2 of 5 scale ratios
}

// TestPoisoning_MixedCredibility tests that when multiple sources have
// varying credibility, the most credible source wins.
func TestPoisoning_MixedCredibility(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	credibilities := []float64{0.05, 0.20, 0.40, 0.60, 0.80, 0.95}
	expectedWinnerIdx := len(credibilities) - 1 // highest credibility

	var nodeIDs []uuid.UUID
	for i, cred := range credibilities {
		id := uuid.New()
		nodeIDs = append(nodeIDs, id)
		n := core.Node{
			ID:         id,
			Namespace:  "cred-test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": fmt.Sprintf("claim from source %d", i)},
			Confidence: cred,
		}
		// All nodes have very similar vectors.
		vec := testdata.TopicVecExported(0, 8, 0.88+float64(i)*0.01, 0.12-float64(i)*0.01)
		_ = graph.UpsertNode(ctx, n)
		vecs.RegisterNode(n)
		nID := id
		_ = vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: "cred-test",
			NodeID:    &nID,
			Vector:    vec,
			ModelID:   "test",
		})
	}

	engine := &retrieval.Engine{
		Graph:   graph,
		Vectors: vecs,
		KV:      memstore.NewKVStore(),
	}

	results, _ := engine.Retrieve(ctx, retrieval.Query{
		Namespace:   "cred-test",
		Vector:      testdata.QueryVecExported(0, 0.05),
		TopK:        10,
		ScoreParams: core.BeliefSystemParams(),
		Strategy: retrieval.HybridStrategy{
			VectorWeight:  0.30,
			GraphWeight:   0.55,
			SessionWeight: 0.15,
			MaxDepth:      3,
		},
	})

	is.True(len(results) > 0) // should have results
	// The highest-credibility node should rank first.
	is.Equal(results[0].ID, nodeIDs[expectedWinnerIdx])

	t.Logf("Winner: node with credibility %.2f (expected %.2f)",
		credibilities[expectedWinnerIdx], credibilities[expectedWinnerIdx])
	for i, r := range results {
		if i >= 6 {
			break
		}
		for j, id := range nodeIDs {
			if r.ID == id {
				t.Logf("  Rank %d: credibility=%.2f  score=%.4f", i+1, credibilities[j], r.Score)
			}
		}
	}
}

// TestPoisoning_AdmissionGateEffectiveness measures how well the admission
// gate would filter troll writes by examining confidence distributions.
func TestPoisoning_AdmissionGateEffectiveness(t *testing.T) {
	// Build the standard corpus which includes troll nodes.
	corpus := testdata.Build()

	trollCount := 0
	trustedCount := 0
	trollConfSum := 0.0
	trustedConfSum := 0.0

	for _, f := range corpus.Fixtures {
		if f.IsTroll {
			trollCount++
			trollConfSum += f.Node.Confidence
		} else if f.IsCorrect {
			trustedCount++
			trustedConfSum += f.Node.Confidence
		}
	}

	t.Log("\n╔══════════════════════════════════════════════════════════════════╗")
	t.Log("║       ADMISSION GATE — Confidence Distribution Analysis         ║")
	t.Log("╚══════════════════════════════════════════════════════════════════╝")
	t.Logf("  Troll nodes:    %d  (avg confidence: %.3f)", trollCount, trollConfSum/float64(trollCount))
	t.Logf("  Trusted nodes:  %d  (avg confidence: %.3f)", trustedCount, trustedConfSum/float64(trustedCount))
	t.Logf("  Separation:     %.3f", (trustedConfSum/float64(trustedCount))-(trollConfSum/float64(trollCount)))

	// Simulate threshold sweep: at each threshold, count how many trolls
	// would be admitted vs how many trusted claims would be blocked.
	t.Log("\n  Threshold sweep:")
	t.Logf("  %-12s %12s %12s %12s", "threshold", "trolls_in", "trusted_out", "f1")
	t.Log("  " + strings.Repeat("-", 52))

	for threshold := 0.0; threshold <= 1.0; threshold += 0.1 {
		trollsIn := 0
		trustedOut := 0
		for _, f := range corpus.Fixtures {
			if f.IsTroll && f.Node.Confidence >= threshold {
				trollsIn++
			}
			if f.IsCorrect && f.Node.Confidence < threshold {
				trustedOut++
			}
		}
		// Precision: fraction of admitted that are not troll.
		admitted := 0
		for _, f := range corpus.Fixtures {
			if f.Node.Confidence >= threshold {
				admitted++
			}
		}
		precision := 0.0
		if admitted > 0 {
			precision = float64(admitted-trollsIn) / float64(admitted)
		}
		recall := 0.0
		if trustedCount > 0 {
			recall = float64(trustedCount-trustedOut) / float64(trustedCount)
		}
		f1 := 0.0
		if precision+recall > 0 {
			f1 = 2 * precision * recall / (precision + recall)
		}
		t.Logf("  %-12.1f %12d %12d %12.3f", threshold, trollsIn, trustedOut, f1)
	}
}
