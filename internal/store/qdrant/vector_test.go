//go:build integration
// +build integration

package qdrant

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

func qdrantAddr() string {
	if addr := os.Getenv("QDRANT_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6334"
}

func TestQdrantVectorIndex(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	vi, err := New(qdrantAddr(), 4)
	is.NoErr(err)
	defer vi.Close()

	ns := "test-" + uuid.NewString()[:8]

	// Index
	nodeID := uuid.New()
	err = vi.Index(ctx, core.VectorEntry{
		ID:        uuid.New(),
		Namespace: ns,
		NodeID:    &nodeID,
		Vector:    []float32{0.1, 0.2, 0.3, 0.4},
		Text:      "test entry",
		CreatedAt: time.Now(),
	})
	is.NoErr(err)

	// Search
	results, err := vi.Search(ctx, store.VectorQuery{
		Namespace: ns,
		Vector:    []float32{0.1, 0.2, 0.3, 0.4},
		TopK:      5,
	})
	is.NoErr(err)
	is.True(len(results) > 0)

	// Delete
	err = vi.Delete(ctx, ns, nodeID)
	is.NoErr(err)
}
