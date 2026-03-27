package ingest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/extract"
	"github.com/antiartificial/contextdb/internal/store"
)

// ConflictDetector identifies contradictions between nodes and creates
// "contradicts" edges. Uses nearest-neighbour candidates from the
// admission check.
type ConflictDetector struct {
	graph store.GraphStore
	llm   extract.Provider // optional — nil means heuristic-only
}

// NewConflictDetector returns a conflict detector.
func NewConflictDetector(graph store.GraphStore, llm extract.Provider) *ConflictDetector {
	return &ConflictDetector{graph: graph, llm: llm}
}

// DetectResult holds the outcome of a conflict scan.
type DetectResult struct {
	ConflictIDs []uuid.UUID
}

// Detect checks whether candidate contradicts any of the nearest neighbours.
// Contradiction candidates are nodes with:
//   - Same labels as the candidate
//   - Moderate similarity (0.3–0.95) — close enough to be on same topic,
//     different enough to potentially disagree
//   - Different source (optional heuristic)
//
// Returns IDs of confirmed conflicting nodes and creates "contradicts" edges.
func (d *ConflictDetector) Detect(ctx context.Context, candidate core.Node, nearest []core.ScoredNode) (DetectResult, error) {
	var result DetectResult

	for _, nn := range nearest {
		sim := nn.SimilarityScore
		// Only moderate-similarity nodes are contradiction candidates
		if sim < 0.3 || sim >= 0.95 {
			continue
		}

		// Must share at least one label
		if !sharesLabel(candidate.Labels, nn.Node.Labels) {
			continue
		}

		// Skip if valid_time windows don't overlap — both claims can be true at different times
		if !temporalOverlap(candidate, nn.Node) {
			continue
		}

		// Determine if it's a contradiction
		weight, err := d.assessContradiction(ctx, candidate, nn.Node)
		if err != nil {
			continue // skip on error
		}

		if weight < 0.5 {
			continue // not a contradiction
		}

		// Create contradicts edge
		if err := d.graph.UpsertEdge(ctx, core.Edge{
			ID:        uuid.New(),
			Namespace: candidate.Namespace,
			Src:       candidate.ID,
			Dst:       nn.Node.ID,
			Type:      "contradicts",
			Weight:    weight,
			ValidFrom: time.Now(),
			TxTime:    time.Now(),
		}); err != nil {
			return result, fmt.Errorf("create contradicts edge: %w", err)
		}

		// Check for interference before decaying confidence
		ifd := InterferenceDetector{graph: d.graph}
		ifResult := ifd.Check(ctx, candidate.Namespace, candidate, nn.Node)
		if ifResult.IsInterference {
			// Still create the contradiction edge, but skip confidence decay
			// The edge records the disagreement; interference means we trust the existing claim
			result.ConflictIDs = append(result.ConflictIDs, nn.Node.ID)
			continue
		}

		// Decay the contradicted node's confidence based on the strength
		// of the contradiction and the candidate's own confidence
		decay := weight * candidate.Confidence * 0.3 // max 30% reduction per contradiction
		nn.Node.Confidence = max(0.1, nn.Node.Confidence-decay)
		if err := d.graph.UpsertNode(ctx, nn.Node); err != nil {
			// Non-fatal — the edge was already created
			_ = err
		}

		result.ConflictIDs = append(result.ConflictIDs, nn.Node.ID)
	}

	return result, nil
}

// temporalOverlap returns true if two nodes' validity windows overlap.
// A nil ValidUntil means "still valid" (open-ended).
func temporalOverlap(a, b core.Node) bool {
	// a starts after b ends
	if b.ValidUntil != nil && a.ValidFrom.After(*b.ValidUntil) {
		return false
	}
	// b starts after a ends
	if a.ValidUntil != nil && b.ValidFrom.After(*a.ValidUntil) {
		return false
	}
	return true
}

// assessContradiction returns P(contradiction) between two nodes.
func (d *ConflictDetector) assessContradiction(ctx context.Context, a, b core.Node) (float64, error) {
	if d.llm != nil {
		return d.llmAssess(ctx, a, b)
	}
	return d.heuristicAssess(a, b), nil
}

// llmAssess uses an LLM to determine if two claims contradict.
func (d *ConflictDetector) llmAssess(ctx context.Context, a, b core.Node) (float64, error) {
	textA := nodeText(a)
	textB := nodeText(b)

	if textA == "" || textB == "" {
		return d.heuristicAssess(a, b), nil
	}

	prompt := fmt.Sprintf(
		"Claim A: %s\nClaim B: %s\n\nDo these two claims contradict each other? "+
			"Answer with a single number from 0.0 (no contradiction) to 1.0 (direct contradiction). "+
			"Output only the number.",
		textA, textB,
	)

	resp, err := d.llm.Chat(ctx, extract.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []extract.ChatMessage{
			{Role: "system", Content: "You are a contradiction detection system. Output only a decimal number between 0.0 and 1.0."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.0,
		MaxTokens:   10,
	})
	if err != nil {
		return d.heuristicAssess(a, b), nil // fallback to heuristic
	}

	var score float64
	content := strings.TrimSpace(resp.Content)
	if _, err := fmt.Sscanf(content, "%f", &score); err != nil {
		return d.heuristicAssess(a, b), nil
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score, nil
}

// heuristicAssess uses simple heuristics for contradiction detection.
func (d *ConflictDetector) heuristicAssess(a, b core.Node) float64 {
	// Same labels + moderate similarity + different content = likely conflict
	labelOverlap := labelOverlapRatio(a.Labels, b.Labels)
	if labelOverlap == 0 {
		return 0
	}

	textA := nodeText(a)
	textB := nodeText(b)

	// If texts are very similar, not a contradiction
	if textA == textB {
		return 0
	}

	// Base score from label overlap
	score := labelOverlap * 0.6

	// Boost if both have text content (suggests they're both claims)
	if textA != "" && textB != "" {
		score += 0.2
	}

	if score > 1 {
		score = 1
	}
	return score
}

func nodeText(n core.Node) string {
	return core.NodeText(n)
}

func sharesLabel(a, b []string) bool {
	for _, la := range a {
		for _, lb := range b {
			if la == lb {
				return true
			}
		}
	}
	return false
}

func labelOverlapRatio(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]bool, len(b))
	for _, l := range b {
		set[l] = true
	}
	overlap := 0
	for _, l := range a {
		if set[l] {
			overlap++
		}
	}
	total := len(a) + len(b) - overlap
	if total == 0 {
		return 0
	}
	return float64(overlap) / float64(total)
}
