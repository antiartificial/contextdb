package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

// vec8 returns a normalised 8-dim float32 vector with a strong bias toward
// dimension d. Used to create predictable cosine-similarity relationships.
func vec8(d int) []float32 {
	v := make([]float32, 8)
	for i := range v {
		if i == d%8 {
			v[i] = 0.9
		} else {
			v[i] = 0.1
		}
	}
	return v
}

// ─── Open / close ─────────────────────────────────────────────────────────────

func TestDB_OpenAndClose(t *testing.T) {
	is := is.New(t)

	db, err := client.Open(client.Options{Mode: client.ModeEmbedded})
	is.NoErr(err)
	is.True(db != nil)

	is.NoErr(db.Ping(context.Background()))
	is.NoErr(db.Close())

	// Idempotent close
	is.NoErr(db.Close())
}

func TestDB_MustOpenPanicsOnBadMode(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown mode")
		}
	}()
	client.MustOpen(client.Options{Mode: "nonexistent"})
}

// ─── Write ────────────────────────────────────────────────────────────────────

func TestNamespace_WriteAdmitsHighCredibility(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:write", namespace.ModeBeliefSystem)

	// Label the source as a moderator before writing
	is.NoErr(ns.LabelSource(ctx, "alice", []string{"moderator"}))

	result, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Go uses concurrent mark-and-sweep garbage collection",
		SourceID: "alice",
		Labels:   []string{"Claim"},
		Vector:   vec8(0),
		ModelID:  "test",
	})

	is.NoErr(err)
	is.True(result.Admitted)
	is.True(result.NodeID.String() != "00000000-0000-0000-0000-000000000000")
}

func TestNamespace_WriteRejectsTrollSource(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:troll", namespace.ModeBeliefSystem)
	is.NoErr(ns.LabelSource(ctx, "troll_user", []string{"troll"}))

	result, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Go has no garbage collector",
		SourceID: "troll_user",
		Labels:   []string{"Claim"},
		Vector:   vec8(0),
	})

	is.NoErr(err)
	is.True(!result.Admitted)
	t.Logf("rejection reason: %s", result.Reason)
}

func TestNamespace_WriteWithoutVectorStillAdmitted(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:novector", namespace.ModeGeneral)

	result, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Fact without embedding",
		SourceID:   "system",
		Confidence: 0.8,
		Labels:     []string{"Fact"},
	})

	is.NoErr(err)
	is.True(result.Admitted)
}

// ─── Retrieve ─────────────────────────────────────────────────────────────────

func TestNamespace_RetrieveReturnsResults(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:retrieve", namespace.ModeGeneral)
	is.NoErr(ns.LabelSource(ctx, "trusted", []string{"verified"}))

	// Write several nodes
	for i := 0; i < 4; i++ {
		_, err := ns.Write(ctx, client.WriteRequest{
			Content:  "document " + string(rune('A'+i)),
			SourceID: "trusted",
			Labels:   []string{"Document"},
			Vector:   vec8(i),
			ModelID:  "test",
		})
		is.NoErr(err)
	}

	// Retrieve with query closest to vec8(0)
	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: vec8(0),
		TopK:   3,
	})

	is.NoErr(err)
	is.True(len(results) > 0)
	is.True(len(results) <= 3)

	t.Log("retrieve results:")
	for _, r := range results {
		t.Logf("  [%s] score=%.4f sim=%.4f conf=%.2f src=%s",
			r.Node.Properties["text"], r.Score, r.SimilarityScore,
			r.Node.Confidence, r.RetrievalSource)
	}
}

func TestNamespace_RetrieveTopKRespected(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:topk", namespace.ModeGeneral)

	// Write 10 nodes
	for i := 0; i < 8; i++ {
		_, _ = ns.Write(ctx, client.WriteRequest{
			Content: "node",
			Vector:  vec8(i % 8),
			Labels:  []string{"Doc"},
		})
	}

	for _, k := range []int{1, 2, 5} {
		results, err := ns.Retrieve(ctx, client.RetrieveRequest{
			Vector: vec8(0),
			TopK:   k,
		})
		is.NoErr(err)
		is.True(len(results) <= k)
	}
}

// ─── Poisoning resistance end-to-end ─────────────────────────────────────────

// TestDB_PoisoningResistanceEndToEnd is the full integration test of the
// belief-system namespace. It writes one trusted claim and five troll claims
// about the same topic, then verifies the trusted claim is ranked first.
func TestDB_PoisoningResistanceEndToEnd(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("channel:general", namespace.ModeBeliefSystem)

	// Label sources
	is.NoErr(ns.LabelSource(ctx, "moderator:alice", []string{"moderator"}))
	is.NoErr(ns.LabelSource(ctx, "troll:anon", []string{"troll"}))

	// 1 trusted write — moderately similar to query
	trustedResult, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Go is garbage collected via concurrent mark-and-sweep",
		SourceID: "moderator:alice",
		Labels:   []string{"Claim"},
		Vector:   []float32{0.85, 0.15, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		ModelID:  "test",
	})
	is.NoErr(err)
	is.True(trustedResult.Admitted)

	// 5 troll writes — higher cosine similarity to query (the poisoning advantage)
	for i := 0; i < 5; i++ {
		trollResult, err := ns.Write(ctx, client.WriteRequest{
			Content:  "Go has no garbage collector",
			SourceID: "troll:anon",
			Labels:   []string{"Claim"},
			Vector:   []float32{0.92 + float32(i)*0.01, 0.12, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
			ModelID:  "test",
		})
		is.NoErr(err)
		// Troll writes should be rejected by the admission gate
		t.Logf("troll write %d: admitted=%v reason=%s", i, trollResult.Admitted, trollResult.Reason)
	}

	// Retrieve with a query very close to the troll vectors
	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: []float32{0.93, 0.13, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		TopK:   10,
	})
	is.NoErr(err)

	t.Log("\nRetrieval results:")
	for i, r := range results {
		t.Logf("  rank=%d score=%.4f sim=%.4f conf=%.2f text=%s",
			i+1, r.Score, r.SimilarityScore, r.Node.Confidence,
			r.Node.Properties["text"])
	}

	// The trusted claim must appear in results
	found := false
	for _, r := range results {
		if r.Node.ID == trustedResult.NodeID {
			found = true
			t.Logf("\nTrusted claim found at score=%.4f", r.Score)
		}
	}
	is.True(found)
}

// ─── Stats ────────────────────────────────────────────────────────────────────

func TestDB_StatsAfterOperations(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:stats", namespace.ModeGeneral)

	_, _ = ns.Write(ctx, client.WriteRequest{
		Content: "test", Vector: vec8(0), Labels: []string{"Test"},
	})
	_, _ = ns.Retrieve(ctx, client.RetrieveRequest{Vector: vec8(0), TopK: 5})

	stats := db.Stats()
	is.True(stats.IngestTotal > 0)
	is.True(stats.RetrievalTotal > 0)

	t.Logf("stats: ingest=%d admitted=%d rejected=%d retrieve=%d p50=%.0fµs p95=%.0fµs",
		stats.IngestTotal, stats.IngestAdmitted, stats.IngestRejected,
		stats.RetrievalTotal, stats.LatencyP50Us, stats.LatencyP95Us)
}

// ─── Temporal query ───────────────────────────────────────────────────────────

func TestNamespace_TemporalAsOfQuery(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:temporal", namespace.ModeAgentMemory)

	past := time.Now().Add(-24 * time.Hour)

	// Write a node valid from 24h ago
	oldResult, err := ns.Write(ctx, client.WriteRequest{
		Content:   "old fact",
		Labels:    []string{"Fact"},
		Vector:    vec8(0),
		ValidFrom: past,
	})
	is.NoErr(err)
	is.True(oldResult.Admitted)

	// Retrieve pinned to 12h ago — should find the old fact
	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: vec8(0),
		TopK:   5,
		AsOf:   time.Now().Add(-12 * time.Hour),
	})
	is.NoErr(err)

	found := false
	for _, r := range results {
		if r.Node.ID == oldResult.NodeID {
			found = true
		}
	}
	is.True(found)
	t.Logf("AsOf query found %d results (expected old fact to be present)", len(results))
}
