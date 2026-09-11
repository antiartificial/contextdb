package dsl

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestPipeExpansionFiltersTraversalEdgesEndToEnd(t *testing.T) {
	ctx := context.Background()
	db := client.MustOpen(client.Options{})
	defer db.Close()

	const ns = "dsl:edge-filter"
	seed, supportsTarget, contradictsTarget := uuid.New(), uuid.New(), uuid.New()
	graph, _, _, _ := db.Stores()
	for _, node := range []core.Node{
		{ID: seed, Namespace: ns, Properties: map[string]any{"text": "seed"}, ValidFrom: time.Now()},
		{ID: supportsTarget, Namespace: ns, Properties: map[string]any{"text": "supported"}, ValidFrom: time.Now()},
		{ID: contradictsTarget, Namespace: ns, Properties: map[string]any{"text": "contradicted"}, ValidFrom: time.Now()},
	} {
		if err := graph.UpsertNode(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	for _, edge := range []core.Edge{
		{ID: uuid.New(), Namespace: ns, Src: seed, Dst: supportsTarget, Type: "supports", Weight: 1, ValidFrom: time.Now()},
		{ID: uuid.New(), Namespace: ns, Src: seed, Dst: contradictsTarget, Type: "contradicts", Weight: 1, ValidFrom: time.Now()},
	} {
		if err := graph.UpsertEdge(ctx, edge); err != nil {
			t.Fatal(err)
		}
	}

	query, err := ParsePipe(`search "claim" | expand supports depth 2 | top 10`)
	if err != nil {
		t.Fatal(err)
	}
	req := ToRetrieveRequest(query)
	req.SeedIDs = []uuid.UUID{seed}
	results, err := db.Namespace(ns, namespace.ModeGeneral).Retrieve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[uuid.UUID]bool)
	for _, result := range results {
		got[result.Node.ID] = true
	}
	if !got[supportsTarget] {
		t.Fatal("supports target missing from DSL traversal")
	}
	if got[contradictsTarget] {
		t.Fatal("contradicts target returned despite DSL edge filter")
	}
}
