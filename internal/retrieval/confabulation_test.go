package retrieval_test

import (
	"context"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/retrieval"
)

func TestCheckGrounding_NilEmbedder_AllGrounded(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	claims := []string{
		"the sky is blue",
		"water is wet",
		"fire is hot",
	}

	// With nil embedder, all claims should be assumed grounded.
	results, err := retrieval.CheckGrounding(ctx, nil, "test:grounding", claims, nil, 0.7)
	is.NoErr(err)
	is.Equal(len(results), len(claims))

	for i, r := range results {
		is.Equal(r.ClaimText, claims[i])
		is.True(r.Grounded)
		is.True(r.BestMatch == nil)
		is.Equal(r.Similarity, 0.0)
	}
}

func TestCheckGrounding_NilVectorIndex_AllGrounded(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	claims := []string{"some claim"}

	// Embedder is non-nil but vecs is nil — should still assume grounded.
	embedder := func(texts []string) ([][]float32, error) {
		vecs := make([][]float32, len(texts))
		for i := range vecs {
			vecs[i] = []float32{0.1, 0.2, 0.3}
		}
		return vecs, nil
	}

	results, err := retrieval.CheckGrounding(ctx, nil, "test:grounding", claims, embedder, 0.7)
	is.NoErr(err)
	is.Equal(len(results), 1)
	is.True(results[0].Grounded)
	is.Equal(results[0].ClaimText, "some claim")
}

func TestCheckGrounding_DefaultThreshold(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	// Zero threshold should default to 0.7 (not panic or error).
	results, err := retrieval.CheckGrounding(ctx, nil, "test:grounding", []string{"claim"}, nil, 0)
	is.NoErr(err)
	is.Equal(len(results), 1)
	is.True(results[0].Grounded)
}

func TestGroundingResult_Fields(t *testing.T) {
	is := is.New(t)

	// Verify the GroundingResult struct fields are accessible and zero-valued by default.
	var r retrieval.GroundingResult
	is.Equal(r.ClaimText, "")
	is.Equal(r.Grounded, false)
	is.True(r.BestMatch == nil)
	is.Equal(r.Similarity, 0.0)

	// Set fields to verify writability.
	r.ClaimText = "test"
	r.Grounded = true
	r.Similarity = 0.95
	is.Equal(r.ClaimText, "test")
	is.True(r.Grounded)
	is.Equal(r.Similarity, 0.95)
}

func TestCheckGrounding_EmptyClaims(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	results, err := retrieval.CheckGrounding(ctx, nil, "test:grounding", []string{}, nil, 0.7)
	is.NoErr(err)
	is.Equal(len(results), 0)
}
