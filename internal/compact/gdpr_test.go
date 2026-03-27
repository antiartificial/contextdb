package compact

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

// newNode is a convenience constructor for test nodes.
func newNode(ns, sourceID string, extraLabels ...string) core.Node {
	labels := append([]string{"Claim"}, extraLabels...)
	props := map[string]any{}
	if sourceID != "" {
		props["source_id"] = sourceID
	}
	return core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    labels,
		Properties: props,
		ValidFrom: time.Now().Add(-time.Hour),
		TxTime:    time.Now(),
	}
}

// ---------------------------------------------------------------------------
// Basic erasure: 2 from target source, 1 from another source
// ---------------------------------------------------------------------------

func TestGDPRProcessor_BasicErasure(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test-gdpr"

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	log := memstore.NewEventLog()

	// Insert 2 nodes belonging to the target source.
	node1 := newNode(ns, "user:123")
	node2 := newNode(ns, "user:123")
	// Insert 1 node belonging to a different source.
	node3 := newNode(ns, "user:999")

	mustUpsert(t, graph, node1)
	mustUpsert(t, graph, node2)
	mustUpsert(t, graph, node3)

	// Register corresponding vector entries.
	nodeID1 := node1.ID
	nodeID2 := node2.ID
	nodeID3 := node3.ID
	is.NoErr(vecs.Index(ctx, core.VectorEntry{ID: nodeID1, Namespace: ns, NodeID: &nodeID1, Vector: []float32{1, 0}}))
	is.NoErr(vecs.Index(ctx, core.VectorEntry{ID: nodeID2, Namespace: ns, NodeID: &nodeID2, Vector: []float32{0, 1}}))
	is.NoErr(vecs.Index(ctx, core.VectorEntry{ID: nodeID3, Namespace: ns, NodeID: &nodeID3, Vector: []float32{1, 1}}))

	proc := NewGDPRProcessor(graph, vecs, kv, log)

	report, err := proc.ProcessErasure(ctx, ErasureRequest{
		Namespace: ns,
		SourceID:  "user:123",
		Reason:    "user requested deletion",
	})
	is.NoErr(err)
	is.True(report != nil)

	// Two nodes should be retracted.
	is.Equal(report.NodesRetracted, 2)
	// Two vectors should be deleted.
	is.Equal(report.VectorsDeleted, 2)
	// No errors.
	is.Equal(len(report.Errors), 0)

	// The third node must still be valid.
	remaining, err := graph.ValidAt(ctx, ns, time.Now(), nil)
	is.NoErr(err)
	found := false
	for _, n := range remaining {
		if n.ID == node3.ID {
			found = true
		}
		// node1 and node2 should NOT appear as currently valid.
		is.True(n.ID != node1.ID)
		is.True(n.ID != node2.ID)
	}
	is.True(found)
}

// ---------------------------------------------------------------------------
// Report counts are accurate
// ---------------------------------------------------------------------------

func TestGDPRProcessor_ReportCounts(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test-gdpr-counts"

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	node := newNode(ns, "user:42")
	mustUpsert(t, graph, node)
	nodeID := node.ID
	is.NoErr(vecs.Index(ctx, core.VectorEntry{ID: nodeID, Namespace: ns, NodeID: &nodeID, Vector: []float32{1, 0}}))

	proc := NewGDPRProcessor(graph, vecs, nil, nil)

	report, err := proc.ProcessErasure(ctx, ErasureRequest{
		Namespace: ns,
		SourceID:  "user:42",
		Reason:    "test",
	})
	is.NoErr(err)
	is.Equal(report.NodesRetracted, 1)
	is.Equal(report.VectorsDeleted, 1)
	is.Equal(report.EdgesInvalidated, 0)
	is.Equal(len(report.NodeIDs), 1)
	is.Equal(report.NodeIDs[0], node.ID)
	is.True(!report.RequestedAt.IsZero())
	is.True(!report.CompletedAt.IsZero())
	is.True(!report.CompletedAt.Before(report.RequestedAt))
	is.Equal(report.Namespace, ns)
	is.Equal(report.SourceID, "user:42")
}

// ---------------------------------------------------------------------------
// No matching nodes → all counts are zero, no error
// ---------------------------------------------------------------------------

func TestGDPRProcessor_NoMatchingNodes(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test-gdpr-empty"

	graph := memstore.NewGraphStore()

	// Node with a different source_id.
	mustUpsert(t, graph, newNode(ns, "user:999"))

	proc := NewGDPRProcessor(graph, nil, nil, nil)

	report, err := proc.ProcessErasure(ctx, ErasureRequest{
		Namespace: ns,
		SourceID:  "user:does-not-exist",
		Reason:    "test",
	})
	is.NoErr(err)
	is.Equal(report.NodesRetracted, 0)
	is.Equal(report.VectorsDeleted, 0)
	is.Equal(report.EdgesInvalidated, 0)
	is.Equal(len(report.NodeIDs), 0)
	is.Equal(len(report.Errors), 0)
}

// ---------------------------------------------------------------------------
// Empty namespace → zero counts, no error
// ---------------------------------------------------------------------------

func TestGDPRProcessor_EmptyNamespace(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	proc := NewGDPRProcessor(graph, nil, nil, nil)

	report, err := proc.ProcessErasure(ctx, ErasureRequest{
		Namespace: "totally-empty",
		SourceID:  "user:123",
		Reason:    "test",
	})
	is.NoErr(err)
	is.Equal(report.NodesRetracted, 0)
	is.Equal(report.VectorsDeleted, 0)
	is.Equal(len(report.Errors), 0)
}

// ---------------------------------------------------------------------------
// Edge invalidation happens for edges from and to target nodes
// ---------------------------------------------------------------------------

func TestGDPRProcessor_EdgeInvalidation(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test-gdpr-edges"

	graph := memstore.NewGraphStore()

	// target node (to be erased) and a bystander node.
	target := newNode(ns, "user:erase-me")
	bystander := newNode(ns, "user:keep-me")
	mustUpsert(t, graph, target)
	mustUpsert(t, graph, bystander)

	// Outgoing edge: target → bystander.
	outEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       target.ID,
		Dst:       bystander.ID,
		Type:      core.EdgeRelatesTo,
		Weight:    1.0,
		ValidFrom: time.Now().Add(-time.Hour),
		TxTime:    time.Now(),
	}
	// Incoming edge: bystander → target.
	inEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       bystander.ID,
		Dst:       target.ID,
		Type:      core.EdgeSupports,
		Weight:    0.8,
		ValidFrom: time.Now().Add(-time.Hour),
		TxTime:    time.Now(),
	}
	is.NoErr(graph.UpsertEdge(ctx, outEdge))
	is.NoErr(graph.UpsertEdge(ctx, inEdge))

	proc := NewGDPRProcessor(graph, nil, nil, nil)

	report, err := proc.ProcessErasure(ctx, ErasureRequest{
		Namespace: ns,
		SourceID:  "user:erase-me",
		Reason:    "edge test",
	})
	is.NoErr(err)
	is.Equal(report.NodesRetracted, 1)
	// Both the outgoing and incoming edges should have been invalidated.
	is.Equal(report.EdgesInvalidated, 2)
	is.Equal(len(report.Errors), 0)

	// Confirm the edges are no longer active from the graph's perspective.
	outgoing, err := graph.GetEdges(ctx, ns, target.ID)
	is.NoErr(err)
	// Only retraction edge remains active (created by RetractNode); the
	// original outEdge should have been invalidated.
	for _, e := range outgoing {
		is.True(e.ID != outEdge.ID)
	}

	incoming, err := graph.GetEdgesTo(ctx, ns, target.ID)
	is.NoErr(err)
	for _, e := range incoming {
		is.True(e.ID != inEdge.ID)
	}
}

// ---------------------------------------------------------------------------
// EffectiveAt honoured: nodes valid after EffectiveAt are excluded
// ---------------------------------------------------------------------------

func TestGDPRProcessor_EffectiveAt(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test-gdpr-effectiveat"

	graph := memstore.NewGraphStore()

	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)

	// A node valid only in the future (not valid at our effective time).
	futureNode := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		Properties: map[string]any{"source_id": "user:future"},
		ValidFrom: future,
		TxTime:    time.Now(),
	}
	// A node already valid (should be found and retracted).
	pastNode := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		Properties: map[string]any{"source_id": "user:future"},
		ValidFrom: past,
		TxTime:    time.Now(),
	}
	mustUpsert(t, graph, futureNode)
	mustUpsert(t, graph, pastNode)

	proc := NewGDPRProcessor(graph, nil, nil, nil)

	// Use "now" as EffectiveAt — futureNode is not valid yet.
	report, err := proc.ProcessErasure(ctx, ErasureRequest{
		Namespace:   ns,
		SourceID:    "user:future",
		Reason:      "effective-at test",
		EffectiveAt: time.Now(),
	})
	is.NoErr(err)
	// Only the pastNode should have been retracted.
	is.Equal(report.NodesRetracted, 1)
	is.Equal(report.NodeIDs[0], pastNode.ID)
}

// ---------------------------------------------------------------------------
// KV cache entry is evicted during erasure
// ---------------------------------------------------------------------------

func TestGDPRProcessor_KVCacheEviction(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test-gdpr-kv"

	graph := memstore.NewGraphStore()
	kv := memstore.NewKVStore()

	// Pre-populate a cache entry for the source.
	cacheKey := ns + ":source:user:789"
	is.NoErr(kv.Set(ctx, cacheKey, []byte("cached data"), 0))

	val, err := kv.Get(ctx, cacheKey)
	is.NoErr(err)
	is.True(val != nil)

	proc := NewGDPRProcessor(graph, nil, kv, nil)

	_, err = proc.ProcessErasure(ctx, ErasureRequest{
		Namespace: ns,
		SourceID:  "user:789",
		Reason:    "kv test",
	})
	is.NoErr(err)

	// The cache entry should now be gone.
	val, err = kv.Get(ctx, cacheKey)
	is.NoErr(err)
	is.True(val == nil)
}

// ---------------------------------------------------------------------------
// nil vecs / kv / log are safe (optional dependencies)
// ---------------------------------------------------------------------------

func TestGDPRProcessor_NilOptionalDeps(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test-gdpr-nil"

	graph := memstore.NewGraphStore()
	mustUpsert(t, graph, newNode(ns, "user:nil-test"))

	// All optional deps are nil — must not panic.
	proc := NewGDPRProcessor(graph, nil, nil, nil)

	report, err := proc.ProcessErasure(ctx, ErasureRequest{
		Namespace: ns,
		SourceID:  "user:nil-test",
		Reason:    "nil deps test",
	})
	is.NoErr(err)
	is.Equal(report.NodesRetracted, 1)
	is.Equal(report.VectorsDeleted, 0) // vecs was nil
}
