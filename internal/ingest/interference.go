package ingest

import (
	"context"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// InterferenceResult indicates whether a potential contradiction is
// actually interference — a low-credibility source trying to overwrite
// well-established knowledge.
type InterferenceResult struct {
	IsInterference bool
	Reason         string
}

// InterferenceDetector checks whether a contradiction candidate is
// interference rather than a genuine disagreement.
type InterferenceDetector struct {
	graph store.GraphStore
}

// NewInterferenceDetector creates an interference detector.
func NewInterferenceDetector(graph store.GraphStore) *InterferenceDetector {
	return &InterferenceDetector{graph: graph}
}

// Check determines if the candidate contradicting the existing node
// represents interference. Interference occurs when:
// 1. The existing node has high confidence (>= 0.8)
// 2. The candidate has low confidence (< 0.4)
// 3. The existing node has strong evidence (multiple supporters)
func (d *InterferenceDetector) Check(ctx context.Context, ns string, candidate, existing core.Node) InterferenceResult {
	existingConf := existing.Confidence
	if existingConf == 0 {
		existingConf = 0.5
	}
	candidateConf := candidate.Confidence
	if candidateConf == 0 {
		candidateConf = 0.5
	}

	// Not interference if existing node isn't well-established
	if existingConf < 0.8 {
		return InterferenceResult{}
	}

	// Not interference if candidate is reasonably credible
	if candidateConf >= 0.4 {
		return InterferenceResult{}
	}

	// Check if existing has strong evidence (>= 2 supporters)
	supporters, err := d.graph.EdgesTo(ctx, ns, existing.ID, []string{core.EdgeSupports})
	if err != nil || len(supporters) < 2 {
		return InterferenceResult{} // not enough evidence to call it interference
	}

	return InterferenceResult{
		IsInterference: true,
		Reason:         "low-credibility source contradicting well-established claim with strong evidence",
	}
}
