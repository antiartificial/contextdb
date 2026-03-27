package retrieval

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

// staticVectorIndex is a test stub that returns a fixed slice of ScoredNodes.
type staticVectorIndex struct {
	results []core.ScoredNode
}

func (s *staticVectorIndex) Index(_ context.Context, _ core.VectorEntry) error { return nil }
func (s *staticVectorIndex) Delete(_ context.Context, _ string, _ uuid.UUID) error {
	return nil
}
func (s *staticVectorIndex) Search(_ context.Context, _ store.VectorQuery) ([]core.ScoredNode, error) {
	return s.results, nil
}

const densityNS = "test:density"

// makeNormVec returns a unit-normalised float32 vector of the given dimension
// with value 1/sqrt(dim) in each component — identical to a query vector so
// cosine similarity == 1.0.
func makeNormVec(dim int) []float32 {
	v := make([]float32, dim)
	val := float32(1.0 / math.Sqrt(float64(dim)))
	for i := range v {
		v[i] = val
	}
	return v
}

// seedDenseFixtures inserts n nodes that are very similar to makeNormVec(8).
func seedDenseFixtures(t *testing.T, vecs *memstore.VectorIndex, n int) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < n; i++ {
		id := uuid.New()
		node := core.Node{
			ID:         id,
			Namespace:  densityNS,
			Labels:     []string{"Concept"},
			Properties: map[string]any{"text": "concept"},
			Confidence: 0.9,
			ValidFrom:  now,
		}
		vecs.RegisterNode(node)
		if err := vecs.Index(ctx, core.VectorEntry{
			ID:        uuid.New(),
			Namespace: densityNS,
			NodeID:    &id,
			Vector:    makeNormVec(8), // identical direction → sim ≈ 1.0
			Text:      "concept",
			ModelID:   "test",
			CreatedAt: now,
		}); err != nil {
			t.Fatalf("index vector: %v", err)
		}
	}
}

// ------------------------------------------------------------------ //
// Tests
// ------------------------------------------------------------------ //

func TestEstimateDensity_EmptyStore(t *testing.T) {
	is := is.New(t)

	vecs := memstore.NewVectorIndex()
	est := NewDensityEstimator(vecs)

	m, err := est.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)
	is.True(m != nil)
	is.Equal(0, m.LocalDensity)
	is.True(m.Sparse)
	is.Equal(0.0, m.AvgSimilarity)
}

func TestEstimateDensity_DenseNeighborhood(t *testing.T) {
	is := is.New(t)

	vecs := memstore.NewVectorIndex()
	seedDenseFixtures(t, vecs, 6)

	est := NewDensityEstimator(vecs)
	m, err := est.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)
	is.True(m != nil)
	is.True(m.LocalDensity >= 3)
	// All vectors are identical to the query, so similarity should be very high.
	is.True(m.AvgSimilarity > 0.9)
	is.True(!m.Sparse)
}

func TestEstimateDensity_SparseNeighborhood(t *testing.T) {
	is := is.New(t)

	// Only 2 results returned — LocalDensity < 3 triggers Sparse.
	stub := &staticVectorIndex{
		results: []core.ScoredNode{
			{Node: core.Node{ValidFrom: time.Now()}, SimilarityScore: 0.6},
			{Node: core.Node{ValidFrom: time.Now()}, SimilarityScore: 0.6},
		},
	}
	est := NewDensityEstimator(stub)
	m, err := est.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)
	is.Equal(2, m.LocalDensity)
	is.True(m.Sparse) // LocalDensity < 3
}

func TestEstimateDensity_LowSimilaritySparse(t *testing.T) {
	is := is.New(t)

	// 5 results but with very low similarity — AvgSimilarity < 0.5 → Sparse.
	now := time.Now()
	results := make([]core.ScoredNode, 5)
	for i := range results {
		results[i] = core.ScoredNode{
			Node:            core.Node{ValidFrom: now},
			SimilarityScore: 0.2,
		}
	}
	stub := &staticVectorIndex{results: results}
	est := NewDensityEstimator(stub)
	m, err := est.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)
	is.True(m.Sparse) // AvgSimilarity (0.2) < 0.5
	is.Equal(5, m.LocalDensity)
	is.True(math.Abs(m.AvgSimilarity-0.2) < 1e-9)
}

func TestEstimateDensity_ConfidenceSpread(t *testing.T) {
	is := is.New(t)

	now := time.Now()
	// Three nodes with distinct confidence values: 0.2, 0.5, 0.8
	// Mean = 0.5, variance = ((0.3^2 + 0^2 + 0.3^2) / 3) = 0.06, stddev ≈ 0.2449
	results := []core.ScoredNode{
		{Node: core.Node{ValidFrom: now}, SimilarityScore: 0.9, ConfidenceScore: 0.2},
		{Node: core.Node{ValidFrom: now}, SimilarityScore: 0.9, ConfidenceScore: 0.5},
		{Node: core.Node{ValidFrom: now}, SimilarityScore: 0.9, ConfidenceScore: 0.8},
	}
	stub := &staticVectorIndex{results: results}
	est := NewDensityEstimator(stub)
	m, err := est.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)

	expectedMean := (0.2 + 0.5 + 0.8) / 3.0
	is.True(math.Abs(m.AvgConfidence-expectedMean) < 1e-9)

	variance := ((0.2-expectedMean)*(0.2-expectedMean) +
		(0.5-expectedMean)*(0.5-expectedMean) +
		(0.8-expectedMean)*(0.8-expectedMean)) / 3.0
	expectedSpread := math.Sqrt(variance)
	is.True(math.Abs(m.ConfidenceSpread-expectedSpread) < 1e-9)
}

func TestEstimateDensity_TemporalCoverage_NewVsOld(t *testing.T) {
	is := is.New(t)

	now := time.Now()
	// fresh: 1h old → freshness ≈ exp(-0.001) ≈ 0.999
	// stale: 720h (30 days) old → freshness ≈ exp(-0.72) ≈ 0.487
	fresh := core.ScoredNode{
		Node:            core.Node{ValidFrom: now.Add(-1 * time.Hour)},
		SimilarityScore: 0.9,
	}
	stale := core.ScoredNode{
		Node:            core.Node{ValidFrom: now.Add(-720 * time.Hour)},
		SimilarityScore: 0.9,
	}

	freshStub := &staticVectorIndex{
		results: []core.ScoredNode{fresh, fresh, fresh},
	}
	staleStub := &staticVectorIndex{
		results: []core.ScoredNode{stale, stale, stale},
	}

	estFresh := NewDensityEstimator(freshStub)
	estStale := NewDensityEstimator(staleStub)

	mFresh, err := estFresh.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)
	mStale, err := estStale.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)

	// Fresh nodes must have higher temporal coverage than stale ones.
	is.True(mFresh.TemporalCoverage > mStale.TemporalCoverage)
	// Fresh coverage should be very close to 1.
	is.True(mFresh.TemporalCoverage > 0.99)
	// Stale coverage should be noticeably less than fresh.
	is.True(mStale.TemporalCoverage < 0.6)
}

func TestEstimateDensity_DefaultTopK(t *testing.T) {
	is := is.New(t)

	// topK=0 should use default of 10; verify it doesn't panic and returns sane results.
	vecs := memstore.NewVectorIndex()
	seedDenseFixtures(t, vecs, 5)
	est := NewDensityEstimator(vecs)

	m, err := est.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 0)
	is.NoErr(err)
	is.True(m != nil)
	is.Equal(5, m.LocalDensity)
}

func TestEstimateDensity_MinSimilarity(t *testing.T) {
	is := is.New(t)

	now := time.Now()
	results := []core.ScoredNode{
		{Node: core.Node{ValidFrom: now}, SimilarityScore: 0.9},
		{Node: core.Node{ValidFrom: now}, SimilarityScore: 0.7},
		{Node: core.Node{ValidFrom: now}, SimilarityScore: 0.6},
	}
	stub := &staticVectorIndex{results: results}
	est := NewDensityEstimator(stub)
	m, err := est.EstimateDensity(context.Background(), densityNS, makeNormVec(8), 10)
	is.NoErr(err)
	is.True(math.Abs(m.MinSimilarity-0.6) < 1e-9)
	is.True(math.Abs(m.AvgSimilarity-(0.9+0.7+0.6)/3.0) < 1e-9)
}
