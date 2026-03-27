package retrieval

import (
	"context"
	"math"
	"time"

	"github.com/antiartificial/contextdb/internal/store"
)

// DensityMetric captures how densely a semantic neighborhood is covered.
type DensityMetric struct {
	// LocalDensity is the number of nodes within TopK nearest neighbors.
	LocalDensity int

	// AvgSimilarity is the mean cosine similarity to the K nearest neighbors.
	AvgSimilarity float64

	// MinSimilarity is the similarity to the most distant of the K neighbors.
	MinSimilarity float64

	// ConfidenceSpread is the std dev of confidence among neighbors.
	ConfidenceSpread float64

	// AvgConfidence is the mean confidence of neighbors.
	AvgConfidence float64

	// TemporalCoverage measures how recent the neighbors are.
	// Range [0, 1]: 1 = all neighbors are fresh, 0 = all very old.
	TemporalCoverage float64

	// Sparse is true if this region is considered under-covered.
	// Heuristic: AvgSimilarity < 0.5 or LocalDensity < 3.
	Sparse bool
}

// DensityEstimator computes semantic neighborhood density.
type DensityEstimator struct {
	vecs store.VectorIndex
}

// NewDensityEstimator creates a density estimator.
func NewDensityEstimator(vecs store.VectorIndex) *DensityEstimator {
	return &DensityEstimator{vecs: vecs}
}

// EstimateDensity measures the density around a query vector.
// topK controls how many neighbors to examine (default 10).
func (d *DensityEstimator) EstimateDensity(ctx context.Context, ns string, vector []float32, topK int) (*DensityMetric, error) {
	if topK <= 0 {
		topK = 10
	}

	results, err := d.vecs.Search(ctx, store.VectorQuery{
		Namespace: ns,
		Vector:    vector,
		TopK:      topK,
	})
	if err != nil {
		return nil, err
	}

	m := &DensityMetric{
		LocalDensity: len(results),
	}

	if len(results) == 0 {
		m.Sparse = true
		return m, nil
	}

	// Compute similarity stats
	var totalSim, totalConf float64
	m.MinSimilarity = 1.0
	now := time.Now()
	var totalFreshness float64

	for _, r := range results {
		sim := r.SimilarityScore
		totalSim += sim
		if sim < m.MinSimilarity {
			m.MinSimilarity = sim
		}

		conf := r.ConfidenceScore
		if conf == 0 {
			conf = 0.5
		}
		totalConf += conf

		// Temporal freshness: exponential decay with 30-day half-life
		age := now.Sub(r.Node.ValidFrom).Hours()
		if age < 0 {
			age = 0
		}
		freshness := math.Exp(-0.001 * age) // ~50% at 693h ≈ 29 days
		totalFreshness += freshness
	}

	n := float64(len(results))
	m.AvgSimilarity = totalSim / n
	m.AvgConfidence = totalConf / n
	m.TemporalCoverage = totalFreshness / n

	// Confidence spread (std dev)
	var sumSqDiff float64
	for _, r := range results {
		conf := r.ConfidenceScore
		if conf == 0 {
			conf = 0.5
		}
		diff := conf - m.AvgConfidence
		sumSqDiff += diff * diff
	}
	m.ConfidenceSpread = math.Sqrt(sumSqDiff / n)

	// Sparse heuristic
	m.Sparse = m.AvgSimilarity < 0.5 || m.LocalDensity < 3

	return m, nil
}
