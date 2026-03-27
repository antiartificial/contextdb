package namespace

import (
	"context"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// HealthScore summarizes the overall quality of a namespace's memory.
type HealthScore struct {
	// Overall is the composite health score [0, 1]. Higher = healthier.
	Overall float64

	// Components break down what contributes to the score.
	AvgConfidence    float64 // average node confidence
	StalenessRatio   float64 // fraction of nodes past ValidUntil
	ConflictRatio    float64 // fraction of nodes with contradicts edges
	SourceDiversity  float64 // unique sources / total nodes (0-1)
	NodeCount        int
	ExpiredCount     int
	ConflictCount    int
	UniqueSourceCount int

	ComputedAt time.Time
}

// ComputeHealth calculates the health score for a namespace.
func ComputeHealth(ctx context.Context, graph store.GraphStore, ns string) (*HealthScore, error) {
	now := time.Now()

	// Get all currently valid nodes
	allNodes, err := graph.ValidAt(ctx, ns, now, nil)
	if err != nil {
		return nil, err
	}

	if len(allNodes) == 0 {
		return &HealthScore{Overall: 1.0, ComputedAt: now}, nil
	}

	h := &HealthScore{
		NodeCount:  len(allNodes),
		ComputedAt: now,
	}

	// Compute metrics
	var totalConf float64
	sourceSet := make(map[string]bool)

	for _, n := range allNodes {
		conf := n.Confidence
		if conf == 0 {
			conf = 0.5
		}
		totalConf += conf

		// Track unique sources
		if sid, ok := n.Properties["source_id"].(string); ok && sid != "" {
			sourceSet[sid] = true
		}

		// Check for expired nodes (ValidUntil set and past)
		if n.ValidUntil != nil && now.After(*n.ValidUntil) {
			h.ExpiredCount++
		}

		// Check for contradictions
		edges, err := graph.EdgesFrom(ctx, ns, n.ID, []string{core.EdgeContradicts})
		if err == nil && len(edges) > 0 {
			h.ConflictCount++
		}
	}

	h.AvgConfidence = totalConf / float64(len(allNodes))
	h.StalenessRatio = float64(h.ExpiredCount) / float64(len(allNodes))
	h.ConflictRatio = float64(h.ConflictCount) / float64(len(allNodes))
	h.UniqueSourceCount = len(sourceSet)
	if len(allNodes) > 0 {
		h.SourceDiversity = float64(len(sourceSet)) / float64(len(allNodes))
		if h.SourceDiversity > 1 {
			h.SourceDiversity = 1
		}
	}

	// Composite: weighted average of component scores
	// Higher avg confidence = better
	// Lower staleness = better
	// Lower conflict ratio = better
	// Higher source diversity = better
	h.Overall = 0.35*h.AvgConfidence +
		0.25*(1.0-h.StalenessRatio) +
		0.20*(1.0-h.ConflictRatio) +
		0.20*h.SourceDiversity

	if h.Overall < 0 {
		h.Overall = 0
	}
	if h.Overall > 1 {
		h.Overall = 1
	}

	return h, nil
}
