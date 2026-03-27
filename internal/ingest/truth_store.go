package ingest

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/store"
)

// TruthEstimateStore persists truth estimates as node properties.
type TruthEstimateStore struct {
	graph store.GraphStore
}

// NewTruthEstimateStore creates a truth estimate store.
func NewTruthEstimateStore(graph store.GraphStore) *TruthEstimateStore {
	return &TruthEstimateStore{graph: graph}
}

// StoredTruthEstimate is the serializable form stored in node Properties.
type StoredTruthEstimate struct {
	Probability float64   `json:"probability"`
	Confidence  float64   `json:"confidence"`
	SourceCount int       `json:"source_count"`
	Method      string    `json:"method"`
	ComputedAt  time.Time `json:"computed_at"`
}

// Persist stores a TruthEstimate on the claim node's Properties["truth_estimate"].
func (s *TruthEstimateStore) Persist(ctx context.Context, ns string, claimID uuid.UUID, est TruthEstimate) error {
	node, err := s.graph.GetNode(ctx, ns, claimID)
	if err != nil || node == nil {
		return err
	}

	if node.Properties == nil {
		node.Properties = make(map[string]any)
	}

	node.Properties["truth_estimate"] = StoredTruthEstimate{
		Probability: est.Probability,
		Confidence:  est.Confidence,
		SourceCount: est.SourceCount,
		Method:      est.Method,
		ComputedAt:  time.Now(),
	}

	return s.graph.UpsertNode(ctx, *node)
}

// Load retrieves a stored TruthEstimate from a node's properties.
// Returns nil if no estimate is stored.
func (s *TruthEstimateStore) Load(ctx context.Context, ns string, claimID uuid.UUID) (*StoredTruthEstimate, error) {
	node, err := s.graph.GetNode(ctx, ns, claimID)
	if err != nil || node == nil {
		return nil, err
	}

	raw, ok := node.Properties["truth_estimate"]
	if !ok {
		return nil, nil
	}

	// Handle both native struct and deserialized map[string]any
	if est, ok := raw.(StoredTruthEstimate); ok {
		return &est, nil
	}
	if m, ok := raw.(map[string]any); ok {
		est := &StoredTruthEstimate{}
		if v, ok := m["probability"].(float64); ok {
			est.Probability = v
		}
		if v, ok := m["confidence"].(float64); ok {
			est.Confidence = v
		}
		if v, ok := m["source_count"].(float64); ok {
			est.SourceCount = int(v)
		}
		if v, ok := m["method"].(string); ok {
			est.Method = v
		}
		return est, nil
	}
	return nil, nil
}
