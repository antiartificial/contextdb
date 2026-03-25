package ingest

import (
	"github.com/ataraxy-labs/contextdb/internal/core"
)

// AdmitRequest carries everything the admission controller needs to decide
// whether a candidate should be written to the graph.
type AdmitRequest struct {
	// The extracted candidate node (not yet persisted).
	Candidate core.Node

	// The asserting source.
	Source core.Source

	// Nearest existing nodes (from a pre-admission graph/vector search).
	// Used to detect near-duplicates.
	NearestNeighbours []core.ScoredNode

	// Threshold: score below this → reject. Usually from namespace config.
	Threshold float64
}

// AdmitDecision is the outcome of the admission check.
type AdmitDecision struct {
	Admit bool

	// ConfidenceMultiplier is applied to the source's effective credibility
	// when writing the node's Confidence field. Range (0, 1].
	ConfidenceMultiplier float64

	// Reason explains why a candidate was rejected (for logging).
	Reason string
}

// Admit decides whether a candidate node should be written to the graph.
//
// Rules (in order):
//  1. Near-duplicate: if an existing node has cosine similarity > 0.95,
//     reinforce it instead of creating a duplicate.
//  2. Source credibility floor: candidates from sources with effective
//     credibility < 0.05 are always rejected.
//  3. Threshold: the combined score (source credibility × novelty) must
//     exceed the namespace admission threshold.
func Admit(req AdmitRequest) AdmitDecision {
	cred := req.Source.EffectiveCredibility()

	// Rule 1 — reject trolls outright
	if cred <= 0.05 {
		return AdmitDecision{
			Admit:  false,
			Reason: "source credibility below floor (< 0.05)",
		}
	}

	// Rule 2 — near-duplicate detection
	for _, nn := range req.NearestNeighbours {
		if nn.SimilarityScore >= 0.95 {
			// reinforce existing instead of duplicating
			return AdmitDecision{
				Admit:  false,
				Reason: "near-duplicate of existing node (similarity ≥ 0.95)",
			}
		}
	}

	// Rule 3 — novelty × credibility must clear threshold
	// novelty is inversely proportional to maximum nearest-neighbour similarity
	maxSim := 0.0
	for _, nn := range req.NearestNeighbours {
		if nn.SimilarityScore > maxSim {
			maxSim = nn.SimilarityScore
		}
	}
	novelty := 1.0 - maxSim
	combinedScore := cred * novelty

	threshold := req.Threshold
	if threshold == 0 {
		threshold = 0.25
	}

	if combinedScore < threshold {
		return AdmitDecision{
			Admit:  false,
			Reason: "combined score below admission threshold",
		}
	}

	return AdmitDecision{
		Admit:                true,
		ConfidenceMultiplier: cred,
	}
}
