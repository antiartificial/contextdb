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

func TestPostgresLexicalCandidatesKeepHistoricalAndLiteralMatches(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer db.Close()
	g, _, _, _ := db.Stores()
	ns := "test:lexical-index:" + uuid.NewString()
	lexical := g.(store.LexicalIndex)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	id := uuid.New()
	n := core.Node{ID: id, Namespace: ns, Properties: map[string]any{"text": "Invoice INV-8Q7A is 50% complete"}, ValidFrom: base, TxTime: base}
	if err := g.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	n.Properties = map[string]any{"text": "Replaced invoice"}
	n.TxTime = base.Add(time.Minute)
	if err := g.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		known time.Time
		want  int
	}{
		{"INV-8Q7A", base.Add(time.Second), 1},
		{"50%", base.Add(time.Second), 1},
		{"INV-8Q7A", base.Add(2 * time.Minute), 0},
	} {
		got, err := lexical.SearchText(ctx, store.TextQuery{Namespace: ns, Text: tc.query, ValidAt: base.Add(2 * time.Minute), KnownAt: tc.known})
		if err != nil || len(got) != tc.want {
			t.Fatalf("query %q known %s: got %d, want %d: %v", tc.query, tc.known, len(got), tc.want, err)
		}
	}
	content := core.Node{ID: uuid.New(), Namespace: ns, Properties: map[string]any{"text": 42, "content": "Fallback%_literal"}, ValidFrom: base, TxTime: base}
	if err := g.UpsertNode(ctx, content); err != nil {
		t.Fatal(err)
	}
	got, err := lexical.SearchText(ctx, store.TextQuery{Namespace: ns, Text: "Fallback%_literal", ValidAt: base.Add(time.Minute), KnownAt: base.Add(time.Minute)})
	if err != nil || len(got) != 1 || got[0].Node.ID != content.ID {
		t.Fatalf("content fallback: %+v, %v", got, err)
	}
}

func TestPostgresRepeatedEdgeEditsPreserveHistoricalWalks(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer db.Close()
	g, _, _, _ := db.Stores()
	ns := "test:edge-history:" + uuid.NewString()
	base := time.Now().Add(-10 * time.Hour).Truncate(time.Second)
	a, b, c, edgeID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{a, b, c} {
		if err := g.UpsertNode(ctx, core.Node{ID: id, Namespace: ns, Labels: []string{"Claim"}, Properties: map[string]any{"text": id.String()}, ValidFrom: base, TxTime: base}); err != nil {
			t.Fatal(err)
		}
	}
	e := core.Edge{ID: edgeID, Namespace: ns, Src: a, Dst: c, Type: "supports", Weight: 1, ValidFrom: base, TxTime: base}
	if err := g.UpsertEdge(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Weight, e.TxTime = .2, base.Add(2*time.Hour)
	if err := g.UpsertEdge(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Src, e.Weight, e.TxTime = b, 1, base.Add(4*time.Hour)
	if err := g.UpsertEdge(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := g.InvalidateEdge(ctx, ns, edgeID, base.Add(6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		seed  uuid.UUID
		known int
		want  int
	}{
		{a, 1, 2}, {a, 3, 1}, {a, 5, 1}, {b, 5, 2}, {b, 7, 1},
	} {
		got, err := g.Walk(ctx, store.WalkQuery{Namespace: ns, SeedIDs: []uuid.UUID{tc.seed}, MaxDepth: 3, AsOf: base.Add(7 * time.Hour), KnownAt: base.Add(time.Duration(tc.known) * time.Hour), MinWeight: .5})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != tc.want {
			t.Fatalf("seed %s known +%dh: got %d, want %d", tc.seed, tc.known, len(got), tc.want)
		}
	}
}

func TestPostgresConcurrentSourceRetractionsAcrossConnections(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	first := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer first.Close()
	second := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer second.Close()
	nsID := "test:source-race:" + uuid.NewString()
	writer := first.Namespace(nsID, namespace.ModeGeneral)
	var nodeID uuid.UUID
	for _, source := range []string{"primary", "source-b", "source-c"} {
		written, err := writer.Write(ctx, client.WriteRequest{Content: "Review key REV-91", SourceID: source, Dedup: true, Confidence: .8})
		if err != nil {
			t.Fatal(err)
		}
		nodeID = written.NodeID
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	at := time.Now().Add(time.Second)
	for _, item := range []struct {
		ns     *client.NamespaceHandle
		source string
	}{
		{first.Namespace(nsID, namespace.ModeGeneral), "source-b"},
		{second.Namespace(nsID, namespace.ModeGeneral), "source-c"},
	} {
		go func(ns *client.NamespaceHandle, source string) {
			<-start
			errs <- ns.RetractSourceAssertion(ctx, nodeID, source, "corrected", at)
		}(item.ns, item.source)
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{"source-b", "source-c"} {
		got, err := writer.Retrieve(ctx, client.RetrieveRequest{Text: "REV-91", ValidAt: at.Add(time.Second), KnownAt: at.Add(time.Second), IncludeSourceIDs: []string{source}})
		if err != nil || len(got) != 0 {
			t.Fatalf("%s still supports claim: %v %+v", source, err, got)
		}
	}
	got, err := writer.Retrieve(ctx, client.RetrieveRequest{Text: "REV-91", ValidAt: at.Add(time.Second), KnownAt: at.Add(time.Second), IncludeSourceIDs: []string{"primary"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("primary support lost: %v %+v", err, got)
	}
}

func TestPostgresConcurrentCorroborationPreservesBothSources(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	first := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer first.Close()
	second := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer second.Close()
	nsID := "test:corroboration-race:" + uuid.NewString()
	ns := first.Namespace(nsID, namespace.ModeGeneral)
	written, err := ns.Write(ctx, client.WriteRequest{Content: "Review key REV-92", SourceID: "primary", Dedup: true, Confidence: .8})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, item := range []struct {
		ns     *client.NamespaceHandle
		source string
	}{
		{ns, "source-b"}, {second.Namespace(nsID, namespace.ModeGeneral), "source-c"},
	} {
		go func(ns *client.NamespaceHandle, source string) {
			<-start
			result, err := ns.Write(ctx, client.WriteRequest{Content: "Review key REV-92", SourceID: source, Dedup: true, Confidence: .7})
			if err == nil && result.NodeID != written.NodeID {
				err = errors.New("concurrent corroboration created another node")
			}
			errs <- err
		}(item.ns, item.source)
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{"primary", "source-b", "source-c"} {
		got, err := ns.Retrieve(ctx, client.RetrieveRequest{Text: "REV-92", IncludeSourceIDs: []string{source}})
		if err != nil || len(got) != 1 {
			t.Fatalf("%s support missing: %v %+v", source, err, got)
		}
	}
}

func TestPostgresAtomicRetractionAuditRollsBackOnEdgeConflict(t *testing.T) {
	dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONTEXTDB_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeStandard, DSN: dsn})
	defer db.Close()
	g, _, _, _ := db.Stores()
	atomic, ok := g.(store.AtomicNodeEdgeStore)
	if !ok {
		t.Fatal("Postgres graph lacks atomic node and edge updates")
	}
	ns := "test:atomic-audit:" + uuid.NewString()
	nodeID, edgeID := uuid.New(), uuid.New()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := g.UpsertNode(ctx, core.Node{ID: nodeID, Namespace: ns, Labels: []string{"Claim"}, Properties: map[string]any{"text": "original"}, ValidFrom: base, TxTime: base}); err != nil {
		t.Fatal(err)
	}
	if err := g.UpsertEdge(ctx, core.Edge{ID: edgeID, Namespace: "other:" + ns, Src: uuid.New(), Dst: uuid.New(), Type: "supports", Weight: 1, ValidFrom: base, TxTime: base}); err != nil {
		t.Fatal(err)
	}
	current, err := g.GetNode(ctx, ns, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	updated := *current
	updated.Properties = map[string]any{"text": "corrected"}
	updated.TxTime = time.Now()
	conflicting := core.Edge{ID: edgeID, Namespace: ns, Src: nodeID, Dst: nodeID, Type: core.EdgeRetracted, Weight: 1, ValidFrom: base, TxTime: time.Now()}
	if written, err := atomic.UpsertNodeAndEdgeIfVersion(ctx, updated, current.Version, conflicting); err == nil || written {
		t.Fatalf("expected edge conflict rollback, got written=%v err=%v", written, err)
	}
	after, err := g.GetNode(ctx, ns, nodeID)
	if err != nil || after.Version != current.Version || after.Properties["text"] != "original" {
		t.Fatalf("node changed despite edge failure: %v %+v", err, after)
	}
	conflicting.ID = uuid.New()
	written, err := atomic.UpsertNodeAndEdgeIfVersion(ctx, updated, current.Version, conflicting)
	if err != nil || !written {
		t.Fatalf("valid atomic update: %v, written=%v", err, written)
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
