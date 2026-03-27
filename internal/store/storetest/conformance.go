// Package storetest provides conformance tests that any store backend must pass.
// Import this package from backend-specific test files and call the Run* functions
// with a factory that creates fresh store instances.
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// RunGraphStoreTests runs the full GraphStore conformance suite.
func RunGraphStoreTests(t *testing.T, factory func(t *testing.T) store.GraphStore) {
	t.Run("UpsertAndGetNode", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		n := core.Node{
			ID:         uuid.New(),
			Namespace:  "test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "hello"},
			Confidence: 0.9,
			ValidFrom:  time.Now().Add(-time.Hour),
			TxTime:     time.Now(),
		}
		is.NoErr(g.UpsertNode(ctx, n))

		got, err := g.GetNode(ctx, "test", n.ID)
		is.NoErr(err)
		is.True(got != nil)
		is.Equal(got.ID, n.ID)
		is.Equal(got.Namespace, "test")
		is.Equal(got.Confidence, 0.9)
	})

	t.Run("GetNodeNotFound", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		got, err := g.GetNode(ctx, "test", uuid.New())
		is.NoErr(err)
		is.True(got == nil)
	})

	t.Run("NodeVersioning", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		id := uuid.New()
		n1 := core.Node{
			ID: id, Namespace: "test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "v1"},
			Confidence: 0.5,
			ValidFrom:  time.Now().Add(-2 * time.Hour),
			TxTime:     time.Now().Add(-2 * time.Hour),
		}
		n2 := core.Node{
			ID: id, Namespace: "test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "v2"},
			Confidence: 0.8,
			ValidFrom:  time.Now().Add(-time.Hour),
			TxTime:     time.Now().Add(-time.Hour),
		}
		is.NoErr(g.UpsertNode(ctx, n1))
		is.NoErr(g.UpsertNode(ctx, n2))

		got, err := g.GetNode(ctx, "test", id)
		is.NoErr(err)
		is.True(got != nil)
		is.Equal(got.Version, uint64(2))

		hist, err := g.History(ctx, "test", id)
		is.NoErr(err)
		is.Equal(len(hist), 2)
		is.Equal(hist[0].Version, uint64(1))
		is.Equal(hist[1].Version, uint64(2))
	})

	t.Run("AsOf", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		id := uuid.New()
		past := time.Now().Add(-3 * time.Hour)
		n := core.Node{
			ID: id, Namespace: "test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "old"},
			Confidence: 0.7,
			ValidFrom:  past,
			TxTime:     past,
		}
		is.NoErr(g.UpsertNode(ctx, n))

		got, err := g.AsOf(ctx, "test", id, past.Add(time.Minute))
		is.NoErr(err)
		is.True(got != nil)

		// Query before valid_from should return nil
		got, err = g.AsOf(ctx, "test", id, past.Add(-time.Hour))
		is.NoErr(err)
		is.True(got == nil)
	})

	t.Run("EdgesFromTo", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		src := uuid.New()
		dst := uuid.New()
		e := core.Edge{
			ID: uuid.New(), Namespace: "test",
			Src: src, Dst: dst,
			Type: "relates_to", Weight: 0.8,
			ValidFrom: time.Now().Add(-time.Hour),
			TxTime:    time.Now(),
		}
		is.NoErr(g.UpsertEdge(ctx, e))

		from, err := g.EdgesFrom(ctx, "test", src, nil)
		is.NoErr(err)
		is.Equal(len(from), 1)
		is.Equal(from[0].Dst, dst)

		to, err := g.EdgesTo(ctx, "test", dst, nil)
		is.NoErr(err)
		is.Equal(len(to), 1)
		is.Equal(to[0].Src, src)
	})

	t.Run("EdgeTypeFilter", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		src := uuid.New()
		e1 := core.Edge{
			ID: uuid.New(), Namespace: "test",
			Src: src, Dst: uuid.New(),
			Type: "relates_to", Weight: 1.0,
			ValidFrom: time.Now().Add(-time.Hour), TxTime: time.Now(),
		}
		e2 := core.Edge{
			ID: uuid.New(), Namespace: "test",
			Src: src, Dst: uuid.New(),
			Type: "contradicts", Weight: 1.0,
			ValidFrom: time.Now().Add(-time.Hour), TxTime: time.Now(),
		}
		is.NoErr(g.UpsertEdge(ctx, e1))
		is.NoErr(g.UpsertEdge(ctx, e2))

		filtered, err := g.EdgesFrom(ctx, "test", src, []string{"contradicts"})
		is.NoErr(err)
		is.Equal(len(filtered), 1)
		is.Equal(filtered[0].Type, "contradicts")
	})

	t.Run("InvalidateEdge", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		src := uuid.New()
		e := core.Edge{
			ID: uuid.New(), Namespace: "test",
			Src: src, Dst: uuid.New(),
			Type: "relates_to", Weight: 1.0,
			ValidFrom: time.Now().Add(-time.Hour), TxTime: time.Now(),
		}
		is.NoErr(g.UpsertEdge(ctx, e))

		is.NoErr(g.InvalidateEdge(ctx, "test", e.ID, time.Now()))

		from, err := g.EdgesFrom(ctx, "test", src, nil)
		is.NoErr(err)
		is.Equal(len(from), 0)
	})

	t.Run("Walk", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		for _, id := range ids {
			is.NoErr(g.UpsertNode(ctx, core.Node{
				ID: id, Namespace: "test",
				Labels:     []string{"Doc"},
				Properties: map[string]any{},
				Confidence: 0.8,
				ValidFrom:  time.Now().Add(-time.Hour),
				TxTime:     time.Now(),
			}))
		}
		is.NoErr(g.UpsertEdge(ctx, core.Edge{
			ID: uuid.New(), Namespace: "test",
			Src: ids[0], Dst: ids[1], Type: "relates_to", Weight: 1.0,
			ValidFrom: time.Now().Add(-time.Hour), TxTime: time.Now(),
		}))
		is.NoErr(g.UpsertEdge(ctx, core.Edge{
			ID: uuid.New(), Namespace: "test",
			Src: ids[1], Dst: ids[2], Type: "relates_to", Weight: 1.0,
			ValidFrom: time.Now().Add(-time.Hour), TxTime: time.Now(),
		}))

		nodes, err := g.Walk(ctx, store.WalkQuery{
			Namespace: "test",
			SeedIDs:   []uuid.UUID{ids[0]},
			MaxDepth:  3,
			Strategy:  store.StrategyBFS,
		})
		is.NoErr(err)
		is.Equal(len(nodes), 3)
	})

	t.Run("SourceCRUD", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		src := core.DefaultSource("test", "user:alice")
		is.NoErr(g.UpsertSource(ctx, src))

		got, err := g.GetSourceByExternalID(ctx, "test", "user:alice")
		is.NoErr(err)
		is.True(got != nil)
		is.Equal(got.ExternalID, "user:alice")
		is.Equal(got.EffectiveCredibility(), 0.5)

		is.NoErr(g.UpdateCredibility(ctx, "test", got.ID, 0.3))
		got2, err := g.GetSourceByExternalID(ctx, "test", "user:alice")
		is.NoErr(err)
		// After credibility update, mean should be closer to 1.0 (delta=0.3)
		is.True(got2.EffectiveCredibility() > 0.5)
	})

	t.Run("RetractNode", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		id := uuid.New()
		now := time.Now()
		n := core.Node{
			ID: id, Namespace: "test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "earth is flat"},
			Confidence: 0.9,
			ValidFrom:  now.Add(-time.Hour),
			TxTime:     now.Add(-time.Hour),
		}
		is.NoErr(g.UpsertNode(ctx, n))

		retractAt := now
		is.NoErr(g.RetractNode(ctx, "test", id, "proven wrong", retractAt))

		// Verify: GetNode returns node with ValidUntil set
		got, err := g.GetNode(ctx, "test", id)
		is.NoErr(err)
		is.True(got != nil)
		is.True(got.ValidUntil != nil)

		// Verify: EdgesFrom returns a "retracted" edge
		edges, err := g.EdgesFrom(ctx, "test", id, []string{"retracted"})
		is.NoErr(err)
		is.Equal(len(edges), 1)
		is.Equal(edges[0].Type, "retracted")
		is.Equal(edges[0].Src, id)
		is.Equal(edges[0].Dst, id)
		is.Equal(edges[0].Properties["reason"], "proven wrong")

		// Verify: History still has the node
		hist, err := g.History(ctx, "test", id)
		is.NoErr(err)
		is.True(len(hist) >= 1)

		// Verify: retract of non-existent node returns error
		err = g.RetractNode(ctx, "test", uuid.New(), "no reason", now)
		is.True(err != nil)
	})

	t.Run("NamespaceIsolation", func(t *testing.T) {
		is := is.New(t)
		g := factory(t)
		ctx := context.Background()

		n := core.Node{
			ID: uuid.New(), Namespace: "ns1",
			Labels: []string{"Claim"}, Properties: map[string]any{},
			Confidence: 0.9, ValidFrom: time.Now().Add(-time.Hour),
			TxTime: time.Now(),
		}
		is.NoErr(g.UpsertNode(ctx, n))

		got, err := g.GetNode(ctx, "ns2", n.ID)
		is.NoErr(err)
		is.True(got == nil)
	})
}

// RunVectorIndexTests runs the VectorIndex conformance suite.
func RunVectorIndexTests(t *testing.T, factory func(t *testing.T) (store.VectorIndex, func(core.Node))) {
	t.Run("IndexAndSearch", func(t *testing.T) {
		is := is.New(t)
		vi, regNode := factory(t)
		ctx := context.Background()

		nodeID := uuid.New()
		n := core.Node{
			ID: nodeID, Namespace: "test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "test"},
			Confidence: 0.9,
			ValidFrom:  time.Now().Add(-time.Hour),
			TxTime:     time.Now(),
		}
		regNode(n)

		is.NoErr(vi.Index(ctx, core.VectorEntry{
			ID: uuid.New(), Namespace: "test",
			NodeID:  &nodeID,
			Vector:  []float32{0.9, 0.1, 0.0, 0.0},
			Text:    "test",
			ModelID: "test-model",
		}))

		results, err := vi.Search(ctx, store.VectorQuery{
			Namespace: "test",
			Vector:    []float32{0.9, 0.1, 0.0, 0.0},
			TopK:      5,
		})
		is.NoErr(err)
		is.True(len(results) > 0)
		is.True(results[0].SimilarityScore > 0.9)
	})

	t.Run("Delete", func(t *testing.T) {
		is := is.New(t)
		vi, regNode := factory(t)
		ctx := context.Background()

		nodeID := uuid.New()
		entryID := uuid.New()
		n := core.Node{
			ID: nodeID, Namespace: "test",
			Labels: []string{"Claim"}, Properties: map[string]any{"text": "test"},
			Confidence: 0.9, ValidFrom: time.Now().Add(-time.Hour), TxTime: time.Now(),
		}
		regNode(n)

		is.NoErr(vi.Index(ctx, core.VectorEntry{
			ID: entryID, Namespace: "test", NodeID: &nodeID,
			Vector: []float32{0.9, 0.1, 0.0, 0.0},
		}))

		is.NoErr(vi.Delete(ctx, "test", entryID))

		results, err := vi.Search(ctx, store.VectorQuery{
			Namespace: "test",
			Vector:    []float32{0.9, 0.1, 0.0, 0.0},
			TopK:      5,
		})
		is.NoErr(err)
		is.Equal(len(results), 0)
	})

	t.Run("TopKRespected", func(t *testing.T) {
		is := is.New(t)
		vi, regNode := factory(t)
		ctx := context.Background()

		for i := 0; i < 10; i++ {
			nodeID := uuid.New()
			n := core.Node{
				ID: nodeID, Namespace: "test",
				Labels: []string{"Claim"}, Properties: map[string]any{},
				Confidence: 0.9, ValidFrom: time.Now().Add(-time.Hour), TxTime: time.Now(),
			}
			regNode(n)
			vec := make([]float32, 4)
			vec[0] = 0.9
			vec[1] = float32(i) * 0.01
			is.NoErr(vi.Index(ctx, core.VectorEntry{
				ID: uuid.New(), Namespace: "test", NodeID: &nodeID,
				Vector: vec,
			}))
		}

		results, err := vi.Search(ctx, store.VectorQuery{
			Namespace: "test",
			Vector:    []float32{0.9, 0.0, 0.0, 0.0},
			TopK:      3,
		})
		is.NoErr(err)
		is.True(len(results) <= 3)
	})
}

// RunKVStoreTests runs the KVStore conformance suite.
func RunKVStoreTests(t *testing.T, factory func(t *testing.T) store.KVStore) {
	t.Run("SetAndGet", func(t *testing.T) {
		is := is.New(t)
		kv := factory(t)
		ctx := context.Background()

		is.NoErr(kv.Set(ctx, "key1", []byte("value1"), 0))
		got, err := kv.Get(ctx, "key1")
		is.NoErr(err)
		is.Equal(string(got), "value1")
	})

	t.Run("GetMissing", func(t *testing.T) {
		is := is.New(t)
		kv := factory(t)
		ctx := context.Background()

		got, err := kv.Get(ctx, "missing")
		is.NoErr(err)
		is.True(got == nil)
	})

	t.Run("Delete", func(t *testing.T) {
		is := is.New(t)
		kv := factory(t)
		ctx := context.Background()

		is.NoErr(kv.Set(ctx, "key1", []byte("value1"), 0))
		is.NoErr(kv.Delete(ctx, "key1"))
		got, err := kv.Get(ctx, "key1")
		is.NoErr(err)
		is.True(got == nil)
	})

	t.Run("Overwrite", func(t *testing.T) {
		is := is.New(t)
		kv := factory(t)
		ctx := context.Background()

		is.NoErr(kv.Set(ctx, "key1", []byte("v1"), 0))
		is.NoErr(kv.Set(ctx, "key1", []byte("v2"), 0))
		got, err := kv.Get(ctx, "key1")
		is.NoErr(err)
		is.Equal(string(got), "v2")
	})
}

// RunEventLogTests runs the EventLog conformance suite.
func RunEventLogTests(t *testing.T, factory func(t *testing.T) store.EventLog) {
	t.Run("AppendAndSince", func(t *testing.T) {
		is := is.New(t)
		el := factory(t)
		ctx := context.Background()

		before := time.Now().Add(-time.Second)
		is.NoErr(el.Append(ctx, store.Event{
			Namespace: "test",
			Type:      store.EventNodeUpsert,
			Payload:   []byte(`{}`),
		}))

		events, err := el.Since(ctx, "test", before)
		is.NoErr(err)
		is.Equal(len(events), 1)
		is.Equal(events[0].Type, store.EventNodeUpsert)
	})

	t.Run("MarkProcessed", func(t *testing.T) {
		is := is.New(t)
		el := factory(t)
		ctx := context.Background()

		before := time.Now().Add(-time.Second)
		is.NoErr(el.Append(ctx, store.Event{
			ID:        uuid.New(),
			Namespace: "test",
			Type:      store.EventNodeUpsert,
			Payload:   []byte(`{}`),
		}))

		events, err := el.Since(ctx, "test", before)
		is.NoErr(err)
		is.Equal(len(events), 1)

		is.NoErr(el.MarkProcessed(ctx, events[0].ID))

		events2, err := el.Since(ctx, "test", before)
		is.NoErr(err)
		is.Equal(len(events2), 0)
	})

	t.Run("NamespaceFilter", func(t *testing.T) {
		is := is.New(t)
		el := factory(t)
		ctx := context.Background()

		before := time.Now().Add(-time.Second)
		is.NoErr(el.Append(ctx, store.Event{
			Namespace: "ns1", Type: store.EventNodeUpsert, Payload: []byte(`{}`),
		}))
		is.NoErr(el.Append(ctx, store.Event{
			Namespace: "ns2", Type: store.EventEdgeUpsert, Payload: []byte(`{}`),
		}))

		events, err := el.Since(ctx, "ns1", before)
		is.NoErr(err)
		is.Equal(len(events), 1)
	})
}
