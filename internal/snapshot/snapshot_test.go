package snapshot_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/snapshot"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

func TestExportImport_RoundTrip(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	ns := "test:snapshot"

	// Set up source store with data
	srcGraph := memstore.NewGraphStore()
	srcVecs := memstore.NewVectorIndex()

	// Write some nodes
	nodeIDs := make([]uuid.UUID, 3)
	for i := 0; i < 3; i++ {
		id := uuid.New()
		nodeIDs[i] = id

		vec := make([]float32, 8)
		vec[i%8] = 0.9
		for j := range vec {
			if j != i%8 {
				vec[j] = 0.1
			}
		}

		node := core.Node{
			ID:         id,
			Namespace:  ns,
			Labels:     []string{"Test"},
			Properties: map[string]any{"text": "node " + id.String()},
			Vector:     vec,
			ModelID:    "test",
			Confidence: 0.8,
			ValidFrom:  time.Now(),
			TxTime:     time.Now(),
		}
		is.NoErr(srcGraph.UpsertNode(ctx, node))

		srcVecs.RegisterNode(node)
		nID := node.ID
		is.NoErr(srcVecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: ns,
			NodeID:    &nID,
			Vector:    vec,
			ModelID:   "test",
			CreatedAt: time.Now(),
		}))
	}

	// Write edges
	for i := 0; i < 2; i++ {
		is.NoErr(srcGraph.UpsertEdge(ctx, core.Edge{
			ID:        uuid.New(),
			Namespace: ns,
			Src:       nodeIDs[i],
			Dst:       nodeIDs[i+1],
			Type:      "relates_to",
			Weight:    0.9,
			ValidFrom: time.Now(),
			TxTime:    time.Now(),
		}))
	}

	// Write a source
	is.NoErr(srcGraph.UpsertSource(ctx, core.Source{
		ID:         uuid.New(),
		Namespace:  ns,
		ExternalID: "alice",
		Labels:     []string{"moderator"},
		Alpha:      9.5, // moderator label overrides anyway
		Beta:       0.5,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}))

	// Export from seeds
	var buf bytes.Buffer
	exporter := snapshot.NewExporter(srcGraph)
	is.NoErr(exporter.ExportFromSeeds(ctx, ns, nodeIDs[:1], 5, &buf))

	t.Logf("exported %d bytes", buf.Len())
	is.True(buf.Len() > 0)

	// Import into a fresh store
	dstGraph := memstore.NewGraphStore()
	dstVecs := memstore.NewVectorIndex()

	importer := snapshot.NewImporter(dstGraph, dstVecs)
	is.NoErr(importer.Import(ctx, ns, &buf))

	// Verify nodes were imported
	for _, id := range nodeIDs {
		node, err := dstGraph.GetNode(ctx, ns, id)
		is.NoErr(err)
		is.True(node != nil)
	}

	// Verify edges were imported
	edges, err := dstGraph.EdgesFrom(ctx, ns, nodeIDs[0], nil)
	is.NoErr(err)
	is.True(len(edges) > 0)
}

func TestImport_EmptyStream(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	importer := snapshot.NewImporter(graph, vecs)
	is.NoErr(importer.Import(ctx, "test", bytes.NewReader(nil)))
}

func TestImport_InvalidJSON(t *testing.T) {
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	importer := snapshot.NewImporter(graph, vecs)
	err := importer.Import(ctx, "test", bytes.NewReader([]byte("not json\n")))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestImport_UnknownRecordType(t *testing.T) {
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()

	importer := snapshot.NewImporter(graph, vecs)
	err := importer.Import(ctx, "test", bytes.NewReader([]byte(`{"type":"unknown","data":{}}`+"\n")))
	if err == nil {
		t.Error("expected error for unknown record type")
	}
}

func TestImport_NamespaceOverride(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	// Create a node in namespace "old"
	srcGraph := memstore.NewGraphStore()
	id := uuid.New()
	is.NoErr(srcGraph.UpsertNode(ctx, core.Node{
		ID:         id,
		Namespace:  "old",
		Labels:     []string{"Test"},
		Properties: map[string]any{"text": "hello"},
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}))

	// Export from seeds
	var buf bytes.Buffer
	exporter := snapshot.NewExporter(srcGraph)
	is.NoErr(exporter.ExportFromSeeds(ctx, "old", []uuid.UUID{id}, 1, &buf))

	// Import into "new" namespace
	dstGraph := memstore.NewGraphStore()
	dstVecs := memstore.NewVectorIndex()

	importer := snapshot.NewImporter(dstGraph, dstVecs)
	is.NoErr(importer.Import(ctx, "new", &buf))

	// The node should be in "new" namespace
	node, err := dstGraph.GetNode(ctx, "new", id)
	is.NoErr(err)
	is.True(node != nil)
	is.Equal(node.Namespace, "new")
}
