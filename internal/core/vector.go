package core

import (
	"time"

	"github.com/google/uuid"
)

// VectorEntry holds an embedding vector, optionally linked to a Node.
// Standalone entries (NodeID == nil) represent document chunks or
// other content that exists outside the graph.
type VectorEntry struct {
	ID        uuid.UUID
	Namespace string
	NodeID    *uuid.UUID // nil = standalone chunk
	Vector    []float32
	Text      string // source text that was embedded
	ModelID   string // embedding model identifier
	CreatedAt time.Time
}

// CosineSimilarity computes the cosine similarity between two float32 slices.
// Returns 0 if either slice is empty or they differ in length.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	denom := sqrt(normA) * sqrt(normB)
	v := dot / denom
	// clamp to [-1, 1] against floating-point drift
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}

// sqrt is an unexported helper to avoid importing math in tests that
// only need similarity.
func sqrt(x float64) float64 {
	if x == 0 {
		return 0
	}
	z := x
	for i := 0; i < 50; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}
