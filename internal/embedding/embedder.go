// Package embedding provides automatic text-to-vector embedding.
// Callers pass raw text; contextdb embeds it automatically using
// a configured Embedder implementation.
package embedding

import "context"

// Embedder converts text into embedding vectors.
type Embedder interface {
	// Embed returns embedding vectors for the given texts.
	// The returned slice has the same length as texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of the embedding vectors
	// produced by this embedder.
	Dimensions() int
}
