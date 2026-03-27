package retrieval

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// ConflictCluster is a group of nodes connected by contradiction edges.
type ConflictCluster struct {
	Nodes []core.Node
	Edges []core.Edge
	// CredibilityGap is the max difference in confidence between any two
	// nodes in the cluster. Higher gaps indicate more interesting conflicts.
	CredibilityGap float64
}

// FindConflictClusters discovers unresolved contradictions among the given
// nodes, groups them into connected components, and ranks by credibility
// imbalance. Pass nil for nodeIDs to scan all currently-valid nodes via ValidAt.
func FindConflictClusters(ctx context.Context, graph store.GraphStore, ns string, nodeIDs []uuid.UUID) ([]ConflictCluster, error) {
	// Get nodes to scan.
	var nodes []core.Node
	if len(nodeIDs) == 0 {
		var err error
		nodes, err = graph.ValidAt(ctx, ns, time.Now(), nil)
		if err != nil {
			return nil, err
		}
	} else {
		for _, id := range nodeIDs {
			n, err := graph.GetNode(ctx, ns, id)
			if err != nil || n == nil {
				continue
			}
			nodes = append(nodes, *n)
		}
	}

	if len(nodes) == 0 {
		return nil, nil
	}

	// Build node lookup by ID.
	nodeMap := make(map[uuid.UUID]core.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	// Collect all active contradicts edges between the candidate nodes.
	var contradictions []core.Edge
	edgeSeen := make(map[uuid.UUID]bool)

	for _, n := range nodes {
		edges, err := graph.EdgesFrom(ctx, ns, n.ID, []string{core.EdgeContradicts})
		if err != nil {
			continue
		}
		for _, e := range edges {
			if edgeSeen[e.ID] {
				continue
			}
			// Only include the edge if the destination is also in our candidate set.
			if _, ok := nodeMap[e.Dst]; ok {
				contradictions = append(contradictions, e)
				edgeSeen[e.ID] = true
			}
		}
	}

	if len(contradictions) == 0 {
		return nil, nil
	}

	// Union-Find to group nodes into connected components via contradiction edges.
	parent := make(map[uuid.UUID]uuid.UUID)

	var find func(uuid.UUID) uuid.UUID
	find = func(x uuid.UUID) uuid.UUID {
		if p, ok := parent[x]; ok && p != x {
			parent[x] = find(p) // path compression
			return parent[x]
		}
		parent[x] = x
		return x
	}
	union := func(a, b uuid.UUID) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	for _, e := range contradictions {
		union(e.Src, e.Dst)
	}

	// Group edges by component root.
	components := make(map[uuid.UUID]*ConflictCluster)
	for _, e := range contradictions {
		root := find(e.Src)
		c, ok := components[root]
		if !ok {
			c = &ConflictCluster{}
			components[root] = c
		}
		c.Edges = append(c.Edges, e)
	}

	// Add nodes to their component. Only nodes that participated in at least
	// one contradiction edge will have an entry in parent.
	for id := range parent {
		root := find(id)
		if c, ok := components[root]; ok {
			if n, ok := nodeMap[id]; ok {
				c.Nodes = append(c.Nodes, n)
			}
		}
	}

	// Compute credibility gap and collect results, dropping degenerate clusters.
	var clusters []ConflictCluster
	for _, c := range components {
		if len(c.Nodes) < 2 {
			continue
		}
		minConf, maxConf := 1.0, 0.0
		for _, n := range c.Nodes {
			conf := n.Confidence
			if conf == 0 {
				conf = 0.5 // treat unset as neutral
			}
			if conf < minConf {
				minConf = conf
			}
			if conf > maxConf {
				maxConf = conf
			}
		}
		c.CredibilityGap = maxConf - minConf
		clusters = append(clusters, *c)
	}

	// Rank by credibility gap descending — most polarised conflicts first.
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].CredibilityGap > clusters[j].CredibilityGap
	})

	return clusters, nil
}
