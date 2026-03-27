package retrieval

import (
	"testing"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
)

func TestMMRRerank(t *testing.T) {
	// Two nodes with identical vectors (cluster A) and two with distinct vectors.
	// Without MMR the cluster-A duplicates dominate the top. With lambda=0.7
	// the diverse nodes should be promoted.
	clusterVec := []float32{1, 0, 0}
	diverseVec1 := []float32{0, 1, 0}
	diverseVec2 := []float32{0, 0, 1}

	results := []core.ScoredNode{
		{Node: core.Node{ID: uuid.New(), Vector: clusterVec}, Score: 0.95},
		{Node: core.Node{ID: uuid.New(), Vector: clusterVec}, Score: 0.90},
		{Node: core.Node{ID: uuid.New(), Vector: diverseVec1}, Score: 0.85},
		{Node: core.Node{ID: uuid.New(), Vector: diverseVec2}, Score: 0.80},
	}

	reranked := mmrRerank(results, 0.7, 4)

	if len(reranked) != 4 {
		t.Fatalf("expected 4 results, got %d", len(reranked))
	}

	// First result should still be the highest-scoring one.
	if reranked[0].Node.ID != results[0].Node.ID {
		t.Error("first result should be the highest-scoring node")
	}

	// Second result should be a diverse node, not the duplicate cluster member.
	// The duplicate (score 0.90) has maxSim=1.0 to selected[0], giving:
	//   mmr = 0.7*0.90 - 0.3*1.0 = 0.33
	// diverseVec1 (score 0.85) has maxSim=0.0 to selected[0], giving:
	//   mmr = 0.7*0.85 - 0.3*0.0 = 0.595
	// So the diverse node should win slot 2.
	if reranked[1].Node.ID == results[1].Node.ID {
		t.Error("second result should NOT be the duplicate cluster member; MMR should promote a diverse node")
	}

	// Verify original scores are preserved (MMR only reorders).
	scoreByID := make(map[uuid.UUID]float64)
	for _, r := range results {
		scoreByID[r.Node.ID] = r.Score
	}
	for _, r := range reranked {
		if r.Score != scoreByID[r.Node.ID] {
			t.Errorf("score changed for node %s: want %f got %f", r.Node.ID, scoreByID[r.Node.ID], r.Score)
		}
	}
}

func TestMMRDisabledWhenZero(t *testing.T) {
	results := []core.ScoredNode{
		{Node: core.Node{ID: uuid.New(), Vector: []float32{1, 0, 0}}, Score: 0.9},
		{Node: core.Node{ID: uuid.New(), Vector: []float32{1, 0, 0}}, Score: 0.8},
		{Node: core.Node{ID: uuid.New(), Vector: []float32{0, 1, 0}}, Score: 0.7},
	}

	reranked := mmrRerank(results, 0, 3)

	if len(reranked) != len(results) {
		t.Fatalf("expected %d results, got %d", len(results), len(reranked))
	}
	for i := range results {
		if reranked[i].Node.ID != results[i].Node.ID {
			t.Errorf("result[%d]: expected node %s, got %s", i, results[i].Node.ID, reranked[i].Node.ID)
		}
	}
}
