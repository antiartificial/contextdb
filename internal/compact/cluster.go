package compact

import (
	"github.com/antiartificial/contextdb/internal/core"
)

// clusterNodes groups nodes by vector similarity using simple agglomerative
// clustering. Returns clusters where each cluster contains nodes with pairwise
// similarity >= threshold.
func clusterNodes(nodes []core.Node, threshold float64, minSize, maxSize int) [][]core.Node {
	if len(nodes) == 0 {
		return nil
	}

	// assigned[i] tracks which cluster each node belongs to (-1 = unassigned)
	assigned := make([]int, len(nodes))
	for i := range assigned {
		assigned[i] = -1
	}

	var clusters [][]core.Node
	clusterIdx := 0

	for i := 0; i < len(nodes); i++ {
		if assigned[i] >= 0 {
			continue
		}
		if len(nodes[i].Vector) == 0 {
			continue
		}

		cluster := []core.Node{nodes[i]}
		assigned[i] = clusterIdx

		for j := i + 1; j < len(nodes); j++ {
			if assigned[j] >= 0 {
				continue
			}
			if len(nodes[j].Vector) == 0 {
				continue
			}
			if len(cluster) >= maxSize {
				break
			}

			// check similarity against the cluster seed (first element)
			sim := core.CosineSimilarity(nodes[i].Vector, nodes[j].Vector)
			if sim >= threshold {
				cluster = append(cluster, nodes[j])
				assigned[j] = clusterIdx
			}
		}

		if len(cluster) >= minSize {
			clusters = append(clusters, cluster)
		}
		clusterIdx++
	}

	return clusters
}

// averageVector computes the centroid of cluster member vectors.
func averageVector(nodes []core.Node) []float32 {
	if len(nodes) == 0 {
		return nil
	}

	dim := 0
	for _, n := range nodes {
		if len(n.Vector) > 0 {
			dim = len(n.Vector)
			break
		}
	}
	if dim == 0 {
		return nil
	}

	avg := make([]float64, dim)
	count := 0
	for _, n := range nodes {
		if len(n.Vector) != dim {
			continue
		}
		for j, v := range n.Vector {
			avg[j] += float64(v)
		}
		count++
	}
	if count == 0 {
		return nil
	}

	result := make([]float32, dim)
	for j := range avg {
		result[j] = float32(avg[j] / float64(count))
	}
	return result
}
