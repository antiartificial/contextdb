package client_test

import (
	"context"
	"sync"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestWriteIdempotencyKeyUsesCanonicalPlan(t *testing.T) {
	is := is.New(t)
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("idempotency", namespace.ModeGeneral)
	req := client.WriteRequest{Content: "stable claim", SourceID: "source", Confidence: 1, IdempotencyKey: "candidate-1"}
	first, err := ns.Write(context.Background(), req)
	is.NoErr(err)
	second, err := ns.Write(context.Background(), req)
	is.NoErr(err)
	is.Equal(second.NodeID, first.NodeID)

	_, err = ns.Write(context.Background(), client.WriteRequest{Content: "changed claim", SourceID: "source", Confidence: 1, IdempotencyKey: "candidate-1"})
	is.True(err != nil)
}

func TestConcurrentIdempotencyKeyWritesOneNode(t *testing.T) {
	is := is.New(t)
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("concurrent-idempotency", namespace.ModeGeneral)
	req := client.WriteRequest{Content: "one claim", SourceID: "source", Confidence: 1, IdempotencyKey: "one"}
	results := make(chan client.WriteResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			result, err := ns.Write(context.Background(), req)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	firstID := ""
	for result := range results {
		if firstID == "" {
			firstID = result.NodeID.String()
		} else {
			is.Equal(result.NodeID.String(), firstID)
		}
	}
	for err := range errs {
		is.NoErr(err)
	}
}
