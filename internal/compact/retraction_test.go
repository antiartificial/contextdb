package compact

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

// ---------------------------------------------------------------------------
// RetractBySource
// ---------------------------------------------------------------------------

func TestBulkRetractor_RetractBySource(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	ns := "test-retract-source"
	now := time.Now()

	// Two nodes from sourceA, one from sourceB.
	nodeA1 := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"source_id": "sourceA"},
		ValidFrom:  now.Add(-time.Hour),
		TxTime:     now.Add(-time.Hour),
	}
	nodeA2 := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"source_id": "sourceA"},
		ValidFrom:  now.Add(-time.Hour),
		TxTime:     now.Add(-time.Hour),
	}
	nodeB := core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"source_id": "sourceB"},
		ValidFrom:  now.Add(-time.Hour),
		TxTime:     now.Add(-time.Hour),
	}

	mustUpsert(t, graph, nodeA1)
	mustUpsert(t, graph, nodeA2)
	mustUpsert(t, graph, nodeB)

	retractor := NewBulkRetractor(graph)
	result, err := retractor.RetractBySource(ctx, ns, "sourceA", "GDPR erasure request")
	is.NoErr(err)
	is.Equal(result.NodesRetracted, 2)
	is.Equal(len(result.NodeIDs), 2)

	// Verify the two sourceA node IDs are in the result.
	retractedSet := make(map[uuid.UUID]bool)
	for _, id := range result.NodeIDs {
		retractedSet[id] = true
	}
	is.True(retractedSet[nodeA1.ID])
	is.True(retractedSet[nodeA2.ID])
	is.True(!retractedSet[nodeB.ID])

	// sourceA nodes should no longer be valid; sourceB should still be valid.
	valid, err := graph.ValidAt(ctx, ns, time.Now(), nil)
	is.NoErr(err)
	validIDs := make(map[uuid.UUID]bool)
	for _, n := range valid {
		validIDs[n.ID] = true
	}
	is.True(!validIDs[nodeA1.ID])
	is.True(!validIDs[nodeA2.ID])
	is.True(validIDs[nodeB.ID])
}

func TestBulkRetractor_RetractBySource_NoMatch(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	ns := "test-no-match"
	now := time.Now()

	mustUpsert(t, graph, core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"source_id": "sourceX"},
		ValidFrom:  now.Add(-time.Hour),
		TxTime:     now.Add(-time.Hour),
	})

	retractor := NewBulkRetractor(graph)
	result, err := retractor.RetractBySource(ctx, ns, "sourceNone", "test")
	is.NoErr(err)
	is.Equal(result.NodesRetracted, 0)
	is.Equal(len(result.NodeIDs), 0)
}

// ---------------------------------------------------------------------------
// RetractByLabel
// ---------------------------------------------------------------------------

func TestBulkRetractor_RetractByLabel(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	ns := "test-retract-label"
	now := time.Now()

	// Two nodes with label "PII", one without.
	nodePII1 := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim", "PII"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}
	nodePII2 := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"PII"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}
	nodeOther := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}

	mustUpsert(t, graph, nodePII1)
	mustUpsert(t, graph, nodePII2)
	mustUpsert(t, graph, nodeOther)

	retractor := NewBulkRetractor(graph)
	result, err := retractor.RetractByLabel(ctx, ns, "PII", "data scrub")
	is.NoErr(err)
	is.Equal(result.NodesRetracted, 2)
	is.Equal(len(result.NodeIDs), 2)

	retractedSet := make(map[uuid.UUID]bool)
	for _, id := range result.NodeIDs {
		retractedSet[id] = true
	}
	is.True(retractedSet[nodePII1.ID])
	is.True(retractedSet[nodePII2.ID])
	is.True(!retractedSet[nodeOther.ID])

	// nodeOther should remain valid.
	valid, err := graph.ValidAt(ctx, ns, time.Now(), nil)
	is.NoErr(err)
	validIDs := make(map[uuid.UUID]bool)
	for _, n := range valid {
		validIDs[n.ID] = true
	}
	is.True(validIDs[nodeOther.ID])
	is.True(!validIDs[nodePII1.ID])
	is.True(!validIDs[nodePII2.ID])
}

func TestBulkRetractor_RetractByLabel_EmptyNamespace(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	retractor := NewBulkRetractor(graph)
	result, err := retractor.RetractByLabel(ctx, "empty-ns", "SomeLabel", "test")
	is.NoErr(err)
	is.Equal(result.NodesRetracted, 0)
}

// ---------------------------------------------------------------------------
// CascadeRetract
// ---------------------------------------------------------------------------

func TestBulkRetractor_CascadeRetract_FullChain(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	ns := "test-cascade"
	now := time.Now()

	// Chain: A derived_from B, B derived_from C.
	// Retracting A should cascade to B and then C.
	nodeA := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}
	nodeB := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}
	nodeC := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}

	mustUpsert(t, graph, nodeA)
	mustUpsert(t, graph, nodeB)
	mustUpsert(t, graph, nodeC)

	// A derived_from B: Src=A, Dst=B
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       nodeA.ID,
		Dst:       nodeB.ID,
		Type:      core.EdgeDerivedFrom,
		Weight:    1.0,
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}))
	// B derived_from C: Src=B, Dst=C
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       nodeB.ID,
		Dst:       nodeC.ID,
		Type:      core.EdgeDerivedFrom,
		Weight:    1.0,
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}))

	retractor := NewBulkRetractor(graph)
	result, err := retractor.CascadeRetract(ctx, ns, nodeA.ID, "cascade test", 0)
	is.NoErr(err)
	is.Equal(result.NodesRetracted, 3)

	retractedSet := make(map[uuid.UUID]bool)
	for _, id := range result.NodeIDs {
		retractedSet[id] = true
	}
	is.True(retractedSet[nodeA.ID])
	is.True(retractedSet[nodeB.ID])
	is.True(retractedSet[nodeC.ID])

	// CascadeDepth should be 2 (A=0, B=1, C=2).
	is.Equal(result.CascadeDepth, 2)
}

func TestBulkRetractor_CascadeRetract_MaxDepth1(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	ns := "test-cascade-depth"
	now := time.Now()

	nodeA := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}
	nodeB := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}
	nodeC := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}

	mustUpsert(t, graph, nodeA)
	mustUpsert(t, graph, nodeB)
	mustUpsert(t, graph, nodeC)

	// A derived_from B, B derived_from C
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       nodeA.ID,
		Dst:       nodeB.ID,
		Type:      core.EdgeDerivedFrom,
		Weight:    1.0,
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}))
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       nodeB.ID,
		Dst:       nodeC.ID,
		Type:      core.EdgeDerivedFrom,
		Weight:    1.0,
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}))

	retractor := NewBulkRetractor(graph)
	// maxDepth=1: retract A (depth 0) and B (depth 1), but NOT C (depth 2).
	result, err := retractor.CascadeRetract(ctx, ns, nodeA.ID, "limited cascade", 1)
	is.NoErr(err)
	is.Equal(result.NodesRetracted, 2)

	retractedSet := make(map[uuid.UUID]bool)
	for _, id := range result.NodeIDs {
		retractedSet[id] = true
	}
	is.True(retractedSet[nodeA.ID])
	is.True(retractedSet[nodeB.ID])
	is.True(!retractedSet[nodeC.ID])

	// nodeC should remain valid.
	valid, err := graph.ValidAt(ctx, ns, time.Now(), nil)
	is.NoErr(err)
	validIDs := make(map[uuid.UUID]bool)
	for _, n := range valid {
		validIDs[n.ID] = true
	}
	is.True(validIDs[nodeC.ID])
}

func TestBulkRetractor_CascadeRetract_NoCycle(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	ns := "test-cascade-cycle"
	now := time.Now()

	// Diamond: A -> B, A -> C, B -> D, C -> D
	nodeA := core.Node{ID: uuid.New(), Namespace: ns, Labels: []string{"Claim"}, ValidFrom: now.Add(-time.Hour), TxTime: now.Add(-time.Hour)}
	nodeB := core.Node{ID: uuid.New(), Namespace: ns, Labels: []string{"Claim"}, ValidFrom: now.Add(-time.Hour), TxTime: now.Add(-time.Hour)}
	nodeC := core.Node{ID: uuid.New(), Namespace: ns, Labels: []string{"Claim"}, ValidFrom: now.Add(-time.Hour), TxTime: now.Add(-time.Hour)}
	nodeD := core.Node{ID: uuid.New(), Namespace: ns, Labels: []string{"Claim"}, ValidFrom: now.Add(-time.Hour), TxTime: now.Add(-time.Hour)}

	mustUpsert(t, graph, nodeA)
	mustUpsert(t, graph, nodeB)
	mustUpsert(t, graph, nodeC)
	mustUpsert(t, graph, nodeD)

	for _, e := range []struct{ src, dst uuid.UUID }{
		{nodeA.ID, nodeB.ID},
		{nodeA.ID, nodeC.ID},
		{nodeB.ID, nodeD.ID},
		{nodeC.ID, nodeD.ID},
	} {
		is.NoErr(graph.UpsertEdge(ctx, core.Edge{
			ID:        uuid.New(),
			Namespace: ns,
			Src:       e.src,
			Dst:       e.dst,
			Type:      core.EdgeDerivedFrom,
			Weight:    1.0,
			ValidFrom: now.Add(-time.Hour),
			TxTime:    now.Add(-time.Hour),
		}))
	}

	retractor := NewBulkRetractor(graph)
	result, err := retractor.CascadeRetract(ctx, ns, nodeA.ID, "diamond test", 0)
	is.NoErr(err)
	// D should only be counted once despite two paths to it.
	is.Equal(result.NodesRetracted, 4)

	ids := make(map[uuid.UUID]bool)
	for _, id := range result.NodeIDs {
		ids[id] = true
	}
	is.True(ids[nodeA.ID])
	is.True(ids[nodeB.ID])
	is.True(ids[nodeC.ID])
	is.True(ids[nodeD.ID])
}

func TestBulkRetractor_CascadeRetract_SingleNode(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	ns := "test-cascade-single"
	now := time.Now()

	node := core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: now.Add(-time.Hour),
		TxTime:    now.Add(-time.Hour),
	}
	mustUpsert(t, graph, node)

	retractor := NewBulkRetractor(graph)
	result, err := retractor.CascadeRetract(ctx, ns, node.ID, "single retract", 0)
	is.NoErr(err)
	is.Equal(result.NodesRetracted, 1)
	is.Equal(result.CascadeDepth, 0)
	is.Equal(len(result.NodeIDs), 1)
	is.Equal(result.NodeIDs[0], node.ID)
}

// ---------------------------------------------------------------------------
// RetractResult NodeIDs ordering — helper for sorted comparison
// ---------------------------------------------------------------------------

func sortedUUIDs(ids []uuid.UUID) []string {
	ss := make([]string, len(ids))
	for i, id := range ids {
		ss[i] = id.String()
	}
	sort.Strings(ss)
	return ss
}
