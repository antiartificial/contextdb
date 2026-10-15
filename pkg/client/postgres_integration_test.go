//go:build integration
// +build integration

package client_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"os"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestPostgresHistoricalLexicalAndSourceLifecycle(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer db.Close()
	nsID := "test:postgres-lifecycle:" + uuid.NewString()
	ns := db.Namespace(nsID, namespace.ModeGeneral)
	first, err := ns.Write(ctx, client.WriteRequest{Content: "Invoice INV-8Q7A was approved", SourceID: "ledger", Dedup: true, Confidence: .9})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ns.Write(ctx, client.WriteRequest{Content: "Invoice INV-8Q7A was approved", SourceID: "audit", Dedup: true, Confidence: .8})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	at := before.Add(time.Second)
	if err := ns.RetractSourceAssertion(ctx, first.NodeID, "ledger", "corrected", at); err != nil {
		t.Fatal(err)
	}
	query := client.RetrieveRequest{Text: "INV-8Q7A", ValidAt: at.Add(time.Second), KnownAt: before, IncludeSourceIDs: []string{"ledger"}}
	old, err := ns.Retrieve(ctx, query)
	if err != nil || len(old) != 1 {
		t.Fatalf("old source: %v %+v", err, old)
	}
	query.KnownAt = at.Add(time.Second)
	removed, err := ns.Retrieve(ctx, query)
	if err != nil || len(removed) != 0 {
		t.Fatalf("removed source: %v %+v", err, removed)
	}
	query.IncludeSourceIDs = []string{"audit"}
	surviving, err := ns.Retrieve(ctx, query)
	if err != nil || len(surviving) != 1 {
		t.Fatalf("surviving source: %v %+v", err, surviving)
	}
	graph, _, _, _ := db.Stores()
	src, dst, edgeID := uuid.New(), uuid.New(), uuid.New()
	base := time.Now().Add(-time.Hour)
	for _, id := range []uuid.UUID{src, dst} {
		if err := graph.UpsertNode(ctx, core.Node{ID: id, Namespace: nsID, Labels: []string{"Claim"}, Properties: map[string]any{"text": id.String()}, ValidFrom: base, TxTime: base}); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.UpsertEdge(ctx, core.Edge{ID: edgeID, Namespace: nsID, Src: src, Dst: dst, Type: "supports", Weight: 1, ValidFrom: base, TxTime: base}); err != nil {
		t.Fatal(err)
	}
	invalidateAt := time.Now().Add(time.Second)
	if err := graph.InvalidateEdge(ctx, nsID, edgeID, invalidateAt); err != nil {
		t.Fatal(err)
	}
	walk := store.WalkQuery{Namespace: nsID, SeedIDs: []uuid.UUID{src}, MaxDepth: 3, AsOf: invalidateAt.Add(time.Second), KnownAt: invalidateAt.Add(-time.Second)}
	prior, err := graph.Walk(ctx, walk)
	if err != nil || len(prior) != 2 {
		t.Fatalf("historical edge: %v %+v", err, prior)
	}
	walk.KnownAt = invalidateAt.Add(time.Second)
	later, err := graph.Walk(ctx, walk)
	if err != nil || len(later) != 1 {
		t.Fatalf("invalidated edge: %v %+v", err, later)
	}
}

func TestPostgresStandardModeWriteRetrieveSmoke(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	db := client.MustOpen(client.Options{
		Mode: client.ModeStandard,
		DSN:  dsn,
	})
	defer db.Close()

	ns := db.Namespace("test:postgres-integration:"+uuid.NewString(), namespace.ModeGeneral)
	validAt := time.Now().Add(-24 * time.Hour)
	knownBeforeWrite := time.Now()
	written, err := ns.Write(ctx, client.WriteRequest{
		Content:   "Postgres integration smoke verifies durable graph and vector paths",
		SourceID:  "ci:postgres",
		Labels:    []string{"Smoke"},
		Vector:    vec8(1),
		ValidFrom: validAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !written.Admitted {
		t.Fatalf("write was rejected: %s", written.Reason)
	}
	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Text:   "durable graph vector paths",
		Vector: vec8(1),
		TopK:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Node.ID != written.NodeID {
		t.Fatalf("retrieve did not return written node: got %d results", len(results))
	}
	before, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: vec8(1), TopK: 1, ValidAt: validAt.Add(time.Hour), KnownAt: knownBeforeWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("late-arriving fact leaked into pre-write knowledge view: %d results", len(before))
	}
	after, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: vec8(1), TopK: 1, ValidAt: validAt.Add(time.Hour), KnownAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Node.ID != written.NodeID {
		t.Fatalf("late-arriving fact missing from post-write knowledge view: %d results", len(after))
	}
}

func TestPostgresReviewLeaseExclusiveAndReleased(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	first := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer first.Close()
	second := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer second.Close()
	firstGraph, _, _, _ := first.Stores()
	secondGraph, _, _, _ := second.Stores()
	firstLease := firstGraph.(store.ReviewLeaseStore)
	secondLease := secondGraph.(store.ReviewLeaseStore)
	release, err := firstLease.AcquireReviewLease(ctx, "test:review-lease")
	if err != nil {
		t.Fatal(err)
	}
	_, err = secondLease.AcquireReviewLease(ctx, "test:review-lease")
	if !errors.Is(err, store.ErrReviewLeaseBusy) {
		t.Fatalf("expected busy lease, got %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, err = secondLease.AcquireReviewLease(ctx, "test:review-lease")
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
