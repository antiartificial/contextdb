package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

func newTestNode(ns string) core.Node {
	return core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "test claim"},
		ValidFrom:  time.Now(),
	}
}

func TestTruthEstimateStore_PersistAndLoad(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	store := NewTruthEstimateStore(graph)

	node := newTestNode("test")
	is.NoErr(graph.UpsertNode(ctx, node))

	est := TruthEstimate{
		ClaimID:     node.ID,
		Probability: 0.82,
		Confidence:  0.75,
		SourceCount: 3,
		Method:      "weighted",
	}

	is.NoErr(store.Persist(ctx, "test", node.ID, est))

	loaded, err := store.Load(ctx, "test", node.ID)
	is.NoErr(err)
	is.True(loaded != nil)
	is.Equal(loaded.Probability, 0.82)
	is.Equal(loaded.Confidence, 0.75)
	is.Equal(loaded.SourceCount, 3)
	is.Equal(loaded.Method, "weighted")
	is.True(!loaded.ComputedAt.IsZero())
}

func TestTruthEstimateStore_LoadMissing(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	store := NewTruthEstimateStore(graph)

	// Node exists but has no truth_estimate property
	node := newTestNode("test")
	is.NoErr(graph.UpsertNode(ctx, node))

	loaded, err := store.Load(ctx, "test", node.ID)
	is.NoErr(err)
	is.True(loaded == nil)
}

func TestTruthEstimateStore_PersistNonexistentNode(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	store := NewTruthEstimateStore(graph)

	est := TruthEstimate{
		Probability: 0.5,
		Confidence:  0.3,
		SourceCount: 1,
		Method:      "weighted",
	}

	// Persist on a node that doesn't exist should return nil (not an error)
	err := store.Persist(ctx, "test", uuid.New(), est)
	is.NoErr(err)
}

func TestTruthEstimateStore_LoadNonexistentNode(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	store := NewTruthEstimateStore(graph)

	loaded, err := store.Load(ctx, "test", uuid.New())
	is.NoErr(err)
	is.True(loaded == nil)
}

func TestTruthEstimateStore_PersistOverwrite(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	store := NewTruthEstimateStore(graph)

	node := newTestNode("test")
	is.NoErr(graph.UpsertNode(ctx, node))

	first := TruthEstimate{
		ClaimID:     node.ID,
		Probability: 0.6,
		Confidence:  0.5,
		SourceCount: 2,
		Method:      "weighted",
	}
	is.NoErr(store.Persist(ctx, "test", node.ID, first))

	second := TruthEstimate{
		ClaimID:     node.ID,
		Probability: 0.9,
		Confidence:  0.85,
		SourceCount: 5,
		Method:      "weighted",
	}
	is.NoErr(store.Persist(ctx, "test", node.ID, second))

	loaded, err := store.Load(ctx, "test", node.ID)
	is.NoErr(err)
	is.True(loaded != nil)
	is.Equal(loaded.Probability, 0.9)
	is.Equal(loaded.SourceCount, 5)
}
