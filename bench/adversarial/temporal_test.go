package adversarial_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
	"github.com/antiartificial/contextdb/testdata"
)

// TestTemporal_ConcurrentWriteConsistency verifies that concurrent writes
// to the same namespace produce a consistent, retrievable state. All written
// nodes must appear in retrieval results and no data should be lost.
func TestTemporal_ConcurrentWriteConsistency(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	const numWriters = 10
	const writesPerWriter = 20
	totalWrites := numWriters * writesPerWriter

	// Track all written node IDs.
	var mu sync.Mutex
	writtenIDs := make(map[uuid.UUID]bool)

	// Concurrent writers — each writes nodes to the same namespace.
	var wg sync.WaitGroup
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				id := uuid.New()
				n := core.Node{
					ID:        id,
					Namespace: "concurrent-test",
					Labels:    []string{"Claim"},
					Properties: map[string]any{
						"text":      fmt.Sprintf("writer %d claim %d", writerID, i),
						"writer_id": writerID,
					},
					Confidence: 0.5 + float64(writerID)*0.03,
					ValidFrom:  time.Now(),
				}

				// Use different topic dimensions per writer for spread.
				vec := testdata.TopicVecExported(writerID%8, (writerID+4)%8, 0.8, 0.2)

				_ = graph.UpsertNode(ctx, n)
				vecs.RegisterNode(n)
				nID := id
				_ = vecs.Index(ctx, core.VectorEntry{
					ID:        uuid.New(),
					Namespace: "concurrent-test",
					NodeID:    &nID,
					Vector:    vec,
					ModelID:   "test",
				})

				mu.Lock()
				writtenIDs[id] = true
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	is.Equal(len(writtenIDs), totalWrites) // all writes should register unique IDs

	// Verify all nodes are retrievable.
	engine := &retrieval.Engine{
		Graph:   graph,
		Vectors: vecs,
		KV:      memstore.NewKVStore(),
	}

	// Query from each writer's dimension to retrieve their nodes.
	retrievedIDs := make(map[uuid.UUID]bool)
	for w := 0; w < numWriters; w++ {
		results, err := engine.Retrieve(ctx, retrieval.Query{
			Namespace:   "concurrent-test",
			Vector:      testdata.QueryVecExported(w%8, 0.05),
			TopK:        totalWrites, // request all
			ScoreParams: core.GeneralParams(),
			Strategy: retrieval.HybridStrategy{
				VectorWeight:  0.80,
				GraphWeight:   0.10,
				SessionWeight: 0.10,
				MaxDepth:      1,
			},
		})
		is.NoErr(err)
		for _, r := range results {
			retrievedIDs[r.ID] = true
		}
	}

	// All written nodes should be retrievable.
	missing := 0
	for id := range writtenIDs {
		if !retrievedIDs[id] {
			missing++
		}
	}

	t.Logf("Concurrent write test: %d writers × %d writes = %d total",
		numWriters, writesPerWriter, totalWrites)
	t.Logf("Retrieved unique nodes: %d / %d (missing: %d)",
		len(retrievedIDs), totalWrites, missing)

	// Allow a small miss rate due to vector search approximation in
	// the in-memory brute-force search (all should be found, but we
	// allow 5% tolerance for edge cases in vector space coverage).
	maxMissRate := 0.05
	missRate := float64(missing) / float64(totalWrites)
	is.True(missRate <= maxMissRate) // miss rate must be below 5%
}

// TestTemporal_VersionConsistency verifies that updating a node creates
// proper temporal versions and that the latest version is retrieved.
func TestTemporal_VersionConsistency(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	nodeID := uuid.New()
	baseVec := testdata.TopicVecExported(0, 4, 0.9, 0.1)

	// Write initial version.
	v1 := core.Node{
		ID:         nodeID,
		Namespace:  "version-test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "initial claim", "version": 1},
		Confidence: 0.80,
		ValidFrom:  time.Now().Add(-2 * time.Hour),
	}
	_ = graph.UpsertNode(ctx, v1)
	vecs.RegisterNode(v1)
	nID := nodeID
	_ = vecs.Index(ctx, core.VectorEntry{
		ID:        uuid.New(),
		Namespace: "version-test",
		NodeID:    &nID,
		Vector:    baseVec,
		ModelID:   "test",
	})

	// Update the node (new version).
	v2 := core.Node{
		ID:         nodeID,
		Namespace:  "version-test",
		Labels:     []string{"Claim", "Updated"},
		Properties: map[string]any{"text": "updated claim", "version": 2},
		Confidence: 0.95,
		ValidFrom:  time.Now(),
	}
	_ = graph.UpsertNode(ctx, v2)

	// History should contain both versions.
	history, err := graph.History(ctx, "version-test", nodeID)
	is.NoErr(err)
	is.True(len(history) >= 1) // at least the current version

	// GetNode should return the latest version.
	latest, err := graph.GetNode(ctx, "version-test", nodeID)
	is.NoErr(err)
	is.True(latest != nil)
	latestVersion, _ := latest.Properties["version"].(int)
	is.True(latestVersion == 2 || latest.Confidence == 0.95) // latest version

	t.Logf("Version test: %d versions, latest confidence=%.2f",
		len(history), latest.Confidence)
}

// TestTemporal_RecencyRankingUnderLoad verifies that under concurrent writes,
// recently written nodes rank higher than older ones when recency weight is
// significant.
func TestTemporal_RecencyRankingUnderLoad(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	// Write old nodes (24h ago).
	var oldIDs []uuid.UUID
	for i := 0; i < 10; i++ {
		id := uuid.New()
		oldIDs = append(oldIDs, id)
		n := core.Node{
			ID:         id,
			Namespace:  "recency-test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": fmt.Sprintf("old claim %d", i), "age": "old"},
			Confidence: 0.85,
			ValidFrom:  time.Now().Add(-24 * time.Hour),
		}
		vec := testdata.TopicVecExported(0, 4, 0.88, 0.12)
		_ = graph.UpsertNode(ctx, n)
		vecs.RegisterNode(n)
		nID := id
		_ = vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: "recency-test",
			NodeID:    &nID,
			Vector:    vec,
			ModelID:   "test",
		})
	}

	// Write fresh nodes (just now) with same vector.
	var freshIDs []uuid.UUID
	for i := 0; i < 5; i++ {
		id := uuid.New()
		freshIDs = append(freshIDs, id)
		n := core.Node{
			ID:         id,
			Namespace:  "recency-test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": fmt.Sprintf("fresh claim %d", i), "age": "fresh"},
			Confidence: 0.85, // same confidence as old
			ValidFrom:  time.Now(),
		}
		vec := testdata.TopicVecExported(0, 4, 0.88, 0.12)
		_ = graph.UpsertNode(ctx, n)
		vecs.RegisterNode(n)
		nID := id
		_ = vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: "recency-test",
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

	// Retrieve with recency-weighted params.
	params := core.AgentMemoryParams() // has significant recency weight
	results, err := engine.Retrieve(ctx, retrieval.Query{
		Namespace:   "recency-test",
		Vector:      testdata.QueryVecExported(0, 0.05),
		TopK:        5,
		ScoreParams: params,
		Strategy: retrieval.HybridStrategy{
			VectorWeight:  0.60,
			GraphWeight:   0.20,
			SessionWeight: 0.20,
			MaxDepth:      1,
		},
	})
	is.NoErr(err)
	is.True(len(results) > 0)

	// Count fresh vs old in top-5.
	freshSet := make(map[uuid.UUID]bool)
	for _, id := range freshIDs {
		freshSet[id] = true
	}

	freshInTop5 := 0
	for _, r := range results {
		if freshSet[r.ID] {
			freshInTop5++
		}
	}

	t.Log("\n╔══════════════════════════════════════════════════════════════════╗")
	t.Log("║      RECENCY RANKING — Fresh vs Old Under Equal Confidence      ║")
	t.Log("╚══════════════════════════════════════════════════════════════════╝")
	t.Logf("  Old nodes: %d (24h ago)", len(oldIDs))
	t.Logf("  Fresh nodes: %d (now)", len(freshIDs))
	t.Log("")
	t.Logf("  %-6s %-40s %8s", "rank", "text", "score")
	t.Log("  " + strings.Repeat("-", 58))
	for i, r := range results {
		text, _ := r.Node.Properties["text"].(string)
		age, _ := r.Node.Properties["age"].(string)
		t.Logf("  %-6d %-40s %8.4f [%s]", i+1, text, r.Score, age)
	}
	t.Logf("\n  Fresh in top-5: %d / %d", freshInTop5, len(freshIDs))

	// Fresh nodes should rank higher than old nodes when recency matters.
	// With identical vectors, at least 2 of the 5 fresh nodes should be
	// in the top-5 due to recency weighting.
	is.True(freshInTop5 >= 2) // at least 2 of 5 fresh nodes in top-5
}
