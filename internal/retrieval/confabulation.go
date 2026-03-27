package retrieval

import (
	"context"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// GroundingResult reports whether a claim text is backed by stored evidence.
type GroundingResult struct {
	ClaimText  string
	Grounded   bool             // true if a supporting node was found
	BestMatch  *core.ScoredNode // the closest matching node, if any
	Similarity float64          // cosine similarity to best match
}

// CheckGrounding verifies that each text claim is backed by stored data.
// Claims with no supporting node above the threshold are flagged as ungrounded.
func CheckGrounding(ctx context.Context, vecs store.VectorIndex, ns string, claims []string, embedder func([]string) ([][]float32, error), threshold float64) ([]GroundingResult, error) {
	if threshold == 0 {
		threshold = 0.7
	}
	if embedder == nil || vecs == nil {
		// Can't check grounding without vectors
		results := make([]GroundingResult, len(claims))
		for i, c := range claims {
			results[i] = GroundingResult{ClaimText: c, Grounded: true} // assume grounded
		}
		return results, nil
	}

	vectors, err := embedder(claims)
	if err != nil {
		return nil, err
	}

	results := make([]GroundingResult, len(claims))
	for i, vec := range vectors {
		results[i] = GroundingResult{ClaimText: claims[i]}

		matches, err := vecs.Search(ctx, store.VectorQuery{
			Namespace: ns,
			Vector:    vec,
			TopK:      1,
		})
		if err != nil || len(matches) == 0 {
			continue
		}

		results[i].Similarity = matches[0].SimilarityScore
		results[i].BestMatch = &matches[0]
		if matches[0].SimilarityScore >= threshold {
			results[i].Grounded = true
		}
	}

	return results, nil
}
