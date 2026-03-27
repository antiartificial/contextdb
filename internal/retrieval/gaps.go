package retrieval

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// KnowledgeGap represents a detected sparse region in the knowledge graph.
type KnowledgeGap struct {
	// ID is a unique identifier for this gap.
	ID uuid.UUID

	// NearestTopics are the text labels/content of the closest known nodes.
	NearestTopics []string

	// CentroidVector is a representative vector for the gap region.
	// Computed as the midpoint between the two most distant neighbors.
	CentroidVector []float32

	// DensityScore is how sparse this region is [0, 1]. Lower = sparser.
	DensityScore float64

	// ConfidenceGap is the average confidence in the sparse region.
	ConfidenceGap float64

	// TemporalGap is the age of the newest node in the region.
	TemporalGap time.Duration
}

// GapDetector identifies knowledge gaps in a namespace.
type GapDetector struct {
	graph store.GraphStore
	vecs  store.VectorIndex
}

// NewGapDetector creates a gap detector.
func NewGapDetector(graph store.GraphStore, vecs store.VectorIndex) *GapDetector {
	return &GapDetector{graph: graph, vecs: vecs}
}

// GapQuery configures gap detection.
type GapQuery struct {
	// TopK is how many seed nodes to sample for gap detection. Default: 20.
	TopK int
	// MinGapSize is the minimum sparsity threshold. Default: 0.5.
	MinGapSize float64
	// MaxGaps is the maximum number of gaps to return. Default: 10.
	MaxGaps int
}

// DetectGaps finds sparse regions in the semantic space of a namespace.
//
// Algorithm:
// 1. Get all currently-valid nodes (or a sample)
// 2. For each pair of nodes with low mutual similarity, compute a midpoint vector
// 3. Search for the midpoint — if few results with low similarity, it's a gap
// 4. Return gaps sorted by sparsity (sparsest first)
func (d *GapDetector) DetectGaps(ctx context.Context, ns string, q GapQuery) ([]KnowledgeGap, error) {
	if q.TopK <= 0 {
		q.TopK = 20
	}
	if q.MinGapSize <= 0 {
		q.MinGapSize = 0.5
	}
	if q.MaxGaps <= 0 {
		q.MaxGaps = 10
	}

	// Get sample of current nodes
	nodes, err := d.graph.ValidAt(ctx, ns, time.Now(), nil)
	if err != nil {
		return nil, err
	}

	if len(nodes) < 2 {
		return nil, nil // not enough data to detect gaps
	}

	// Filter to nodes with vectors
	var withVectors []core.Node
	for _, n := range nodes {
		if len(n.Vector) > 0 {
			withVectors = append(withVectors, n)
		}
	}
	if len(withVectors) < 2 {
		return nil, nil
	}

	// Limit sample size
	sample := withVectors
	if len(sample) > q.TopK {
		sample = sample[:q.TopK]
	}

	// Find pairs with low mutual similarity — these have gaps between them
	var gaps []KnowledgeGap
	for i := 0; i < len(sample) && len(gaps) < q.MaxGaps*2; i++ {
		for j := i + 1; j < len(sample) && len(gaps) < q.MaxGaps*2; j++ {
			sim := core.CosineSimilarity(sample[i].Vector, sample[j].Vector)

			// Only consider pairs that are semantically distant (low similarity)
			// but not completely unrelated
			if sim > 0.6 || sim < 0.1 {
				continue
			}

			// Compute midpoint vector
			midpoint := vectorMidpoint(sample[i].Vector, sample[j].Vector)

			// Search for the midpoint — if few/distant results, it's a gap
			results, err := d.vecs.Search(ctx, store.VectorQuery{
				Namespace: ns,
				Vector:    midpoint,
				TopK:      5,
			})
			if err != nil {
				continue
			}

			// Compute gap metrics
			var avgSim, avgConf float64
			var newestAge time.Duration
			now := time.Now()

			for _, r := range results {
				avgSim += r.SimilarityScore
				conf := r.ConfidenceScore
				if conf == 0 {
					conf = 0.5
				}
				avgConf += conf
				age := now.Sub(r.Node.ValidFrom)
				if age > newestAge {
					newestAge = age
				}
			}

			if len(results) > 0 {
				avgSim /= float64(len(results))
				avgConf /= float64(len(results))
			}

			density := avgSim // use average similarity as density proxy
			if density >= q.MinGapSize {
				continue // not sparse enough
			}

			// Extract topic labels from the neighboring nodes
			var topics []string
			topicA := nodeTopicText(sample[i])
			topicB := nodeTopicText(sample[j])
			if topicA != "" {
				topics = append(topics, topicA)
			}
			if topicB != "" {
				topics = append(topics, topicB)
			}

			gaps = append(gaps, KnowledgeGap{
				ID:             uuid.New(),
				NearestTopics:  topics,
				CentroidVector: midpoint,
				DensityScore:   density,
				ConfidenceGap:  avgConf,
				TemporalGap:    newestAge,
			})
		}
	}

	// Sort by density (sparsest first)
	sortGapsByDensity(gaps)

	// Limit to MaxGaps
	if len(gaps) > q.MaxGaps {
		gaps = gaps[:q.MaxGaps]
	}

	return gaps, nil
}

func vectorMidpoint(a, b []float32) []float32 {
	if len(a) != len(b) {
		return a
	}
	mid := make([]float32, len(a))
	for i := range a {
		mid[i] = (a[i] + b[i]) / 2
	}
	// Normalize
	var norm float64
	for _, v := range mid {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range mid {
			mid[i] = float32(float64(mid[i]) / norm)
		}
	}
	return mid
}

func sortGapsByDensity(gaps []KnowledgeGap) {
	for i := 1; i < len(gaps); i++ {
		for j := i; j > 0 && gaps[j].DensityScore < gaps[j-1].DensityScore; j-- {
			gaps[j], gaps[j-1] = gaps[j-1], gaps[j]
		}
	}
}

func nodeTopicText(n core.Node) string {
	if t, ok := n.Properties["text"].(string); ok {
		return t
	}
	if t, ok := n.Properties["content"].(string); ok {
		return t
	}
	if len(n.Labels) > 0 {
		return n.Labels[0]
	}
	return ""
}
