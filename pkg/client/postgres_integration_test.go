//go:build integration
// +build integration

package client_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"os"
	"testing"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/pkg/client"
)

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
	written, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Postgres integration smoke verifies durable graph and vector paths",
		SourceID: "ci:postgres",
		Labels:   []string{"Smoke"},
		Vector:   vec8(1),
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
