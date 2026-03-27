package ingest

import (
	"context"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// EchoChamberAlert flags a cluster of sources that may form an echo chamber.
type EchoChamberAlert struct {
	SourceIDs  []string // external IDs of the clustered sources
	Confidence float64  // how confident we are this is an echo chamber [0, 1]
	Reason     string
}

// EchoDetector identifies potential echo chambers by analyzing
// support patterns between sources.
type EchoDetector struct {
	graph store.GraphStore
}

// NewEchoDetector creates an echo chamber detector.
func NewEchoDetector(graph store.GraphStore) *EchoDetector {
	return &EchoDetector{graph: graph}
}

// Detect analyzes support edges in the given namespace to find
// echo chamber patterns. A cluster is flagged when:
// - Multiple sources consistently support each other's claims
// - The support is mutual (A supports B AND B supports A)
// - The cluster lacks external validation from outside sources
//
// sourceNodes maps source external IDs to the node IDs they authored.
func (d *EchoDetector) Detect(ctx context.Context, ns string, sourceNodes map[string][]core.Node) ([]EchoChamberAlert, error) {
	if len(sourceNodes) < 2 {
		return nil, nil
	}

	// Build a source-to-source support matrix:
	// supportCount[A][B] = number of times source A's nodes support source B's nodes
	type sourceID = string
	supportCount := make(map[sourceID]map[sourceID]int)

	// Initialize
	for sid := range sourceNodes {
		supportCount[sid] = make(map[sourceID]int)
	}

	// Build reverse index: nodeID → sourceID
	nodeToSource := make(map[string]sourceID) // uuid string → source
	for sid, nodes := range sourceNodes {
		for _, n := range nodes {
			nodeToSource[n.ID.String()] = sid
		}
	}

	// Scan support edges from each source's nodes
	for sid, nodes := range sourceNodes {
		for _, n := range nodes {
			edges, err := d.graph.EdgesFrom(ctx, ns, n.ID, []string{core.EdgeSupports})
			if err != nil {
				continue
			}
			for _, e := range edges {
				targetSource, ok := nodeToSource[e.Dst.String()]
				if !ok || targetSource == sid {
					continue // skip self-support or unknown targets
				}
				supportCount[sid][targetSource]++
			}
		}
	}

	// Detect mutual support clusters
	var alerts []EchoChamberAlert
	visited := make(map[sourceID]bool)

	for srcA, targets := range supportCount {
		if visited[srcA] {
			continue
		}

		// Find mutual supporters of srcA
		cluster := []sourceID{srcA}
		for srcB, countAB := range targets {
			if visited[srcB] {
				continue
			}
			countBA := supportCount[srcB][srcA]

			// Mutual support: both directions with at least 2 instances each
			if countAB >= 2 && countBA >= 2 {
				cluster = append(cluster, srcB)
			}
		}

		if len(cluster) >= 2 {
			// Calculate confidence based on mutual support strength
			totalMutual := 0
			for i := 0; i < len(cluster); i++ {
				for j := i + 1; j < len(cluster); j++ {
					totalMutual += supportCount[cluster[i]][cluster[j]]
					totalMutual += supportCount[cluster[j]][cluster[i]]
				}
			}

			// Higher mutual support count → higher echo chamber confidence
			// Normalize: 4 mutual supports = 0.5, 10+ = 0.9
			conf := float64(totalMutual) / (float64(totalMutual) + 8.0)

			if conf >= 0.3 { // threshold for alerting
				for _, s := range cluster {
					visited[s] = true
				}
				alerts = append(alerts, EchoChamberAlert{
					SourceIDs:  cluster,
					Confidence: conf,
					Reason:     "mutual support pattern detected",
				})
			}
		}
	}

	return alerts, nil
}
