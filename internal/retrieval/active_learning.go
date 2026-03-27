package retrieval

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// AcquisitionType classifies what kind of information should be acquired.
type AcquisitionType string

const (
	AcquireVerifyClaim   AcquisitionType = "verify_claim"   // existing claim needs validation
	AcquireRefreshStale  AcquisitionType = "refresh_stale"  // stale information needs updating
	AcquireLowConfidence AcquisitionType = "low_confidence" // uncertain claim needs evidence
	AcquireHighUtility   AcquisitionType = "high_utility"   // frequently-accessed but poorly sourced
)

// AcquisitionSuggestion recommends an information acquisition action.
type AcquisitionSuggestion struct {
	Type           AcquisitionType
	Priority       float64 // [0, 1]: higher = more urgent
	Description    string
	RelatedNodeIDs []uuid.UUID
	Namespace      string
}

// ActiveLearner generates prioritized suggestions for what to learn next.
type ActiveLearner struct {
	graph store.GraphStore
}

// NewActiveLearner creates an active learner.
func NewActiveLearner(graph store.GraphStore) *ActiveLearner {
	return &ActiveLearner{graph: graph}
}

// Suggest returns prioritized acquisition suggestions for a namespace.
// Budget limits the number of suggestions returned.
func (l *ActiveLearner) Suggest(ctx context.Context, ns string, budget int) ([]AcquisitionSuggestion, error) {
	if budget <= 0 {
		budget = 10
	}

	nodes, err := l.graph.ValidAt(ctx, ns, time.Now(), nil)
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return nil, nil
	}

	var suggestions []AcquisitionSuggestion
	now := time.Now()

	for _, n := range nodes {
		conf := n.Confidence
		if conf == 0 {
			conf = 0.5
		}

		// 1. Low confidence claims that might benefit from verification
		if conf < 0.4 {
			text := activeNodeText(n)
			suggestions = append(suggestions, AcquisitionSuggestion{
				Type:           AcquireLowConfidence,
				Priority:       1.0 - conf, // lower confidence = higher priority
				Description:    "Low confidence claim needs supporting evidence: " + activeTruncate(text, 100),
				RelatedNodeIDs: []uuid.UUID{n.ID},
				Namespace:      ns,
			})
		}

		// 2. Stale claims approaching expiry
		if n.ValidUntil != nil {
			hoursUntilExpiry := n.ValidUntil.Sub(now).Hours()
			if hoursUntilExpiry > 0 && hoursUntilExpiry < 168 { // within 7 days
				priority := 1.0 - (hoursUntilExpiry / 168.0)
				text := activeNodeText(n)
				suggestions = append(suggestions, AcquisitionSuggestion{
					Type:           AcquireRefreshStale,
					Priority:       priority,
					Description:    "Claim expiring soon, needs refresh: " + activeTruncate(text, 100),
					RelatedNodeIDs: []uuid.UUID{n.ID},
					Namespace:      ns,
				})
			}
		}

		// 3. Old claims with high confidence but no recent validation
		age := now.Sub(n.ValidFrom).Hours()
		if conf >= 0.7 && age > 720 { // 30+ days old with high confidence
			// Check if it has contradictions — if so, it needs verification
			edges, err := l.graph.EdgesFrom(ctx, ns, n.ID, []string{core.EdgeContradicts})
			if err == nil && len(edges) > 0 {
				text := activeNodeText(n)
				suggestions = append(suggestions, AcquisitionSuggestion{
					Type:           AcquireVerifyClaim,
					Priority:       0.7,
					Description:    "Old high-confidence claim with active contradictions: " + activeTruncate(text, 100),
					RelatedNodeIDs: []uuid.UUID{n.ID},
					Namespace:      ns,
				})
			}
		}
	}

	// Sort by priority (highest first)
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Priority > suggestions[j].Priority
	})

	// Limit to budget
	if len(suggestions) > budget {
		suggestions = suggestions[:budget]
	}

	return suggestions, nil
}

// activeNodeText extracts the human-readable text from a node's properties.
func activeNodeText(n core.Node) string {
	if t, ok := n.Properties["text"].(string); ok {
		return t
	}
	if t, ok := n.Properties["content"].(string); ok {
		return t
	}
	return ""
}

// activeTruncate shortens s to at most maxLen characters, appending "..." if truncated.
func activeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
