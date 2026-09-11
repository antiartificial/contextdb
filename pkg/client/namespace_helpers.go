package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/ingest"
	"github.com/google/uuid"
	"strings"
	"time"
)

func writeRequestHash(req WriteRequest, nodeID uuid.UUID) string {
	payload, _ := json.Marshal(struct {
		NodeID     uuid.UUID      `json:"node_id"`
		Content    string         `json:"content"`
		SourceID   string         `json:"source_id"`
		Labels     []string       `json:"labels"`
		Properties map[string]any `json:"properties"`
		Vector     []float32      `json:"vector"`
		ModelID    string         `json:"model_id"`
		Confidence float64        `json:"confidence"`
		ValidFrom  time.Time      `json:"valid_from"`
	}{nodeID, req.Content, req.SourceID, req.Labels, req.Properties, req.Vector, req.ModelID, req.Confidence, req.ValidFrom})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Result is a single retrieval result with its score breakdown.
type Result struct {
	Node core.Node

	// Score is the composite retrieval score [0, 1].
	Score float64

	// Components expose why this node ranked where it did.
	SimilarityScore float64
	ConfidenceScore float64
	RecencyScore    float64
	UtilityScore    float64
	Breakdown       core.ScoreBreakdown

	// RetrievalSource indicates which path(s) found this node.
	RetrievalSource string
}

func (h *NamespaceHandle) resolveSource(ctx context.Context, externalID string) (core.Source, error) {
	if externalID == "" {
		return core.DefaultSource(h.cfg.ID, "anonymous"), nil
	}
	existing, err := h.db.graph.GetSourceByExternalID(ctx, h.cfg.ID, externalID)
	if err != nil {
		return core.Source{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	src := core.DefaultSource(h.cfg.ID, externalID)
	if err := h.db.graph.UpsertSource(ctx, src); err != nil {
		return core.Source{}, err
	}
	return src, nil
}

// LabelSource sets labels on a source to apply credibility overrides.
// Use "moderator" or "admin" for full trust, "troll" or "flagged" for floor.
func (h *NamespaceHandle) LabelSource(ctx context.Context, externalID string, labels []string) error {
	src, err := h.resolveSource(ctx, externalID)
	if err != nil {
		return err
	}
	src.Labels = labels
	return h.db.graph.UpsertSource(ctx, src)
}

// Consensus resolves the truth estimate for a claim node by aggregating all
// source assertions recorded against that claim ID. It uses a credibility-
// weighted vote (see ingest.MultiSourceConsensus). Domain-scoped credibility
// is applied automatically when the claim node carries a "domain" property or
// labels that can be used as a domain proxy.
func (h *NamespaceHandle) Consensus(ctx context.Context, claimID uuid.UUID) (*ingest.TruthEstimate, error) {
	resolver := ingest.NewConsensusResolver(h.db.graph, h.db.logger)
	est, err := resolver.ResolveTruth(ctx, claimID)
	if err != nil {
		return nil, fmt.Errorf("consensus: %w", err)
	}
	return &est, nil
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func nodeSourceID(node core.Node) string {
	sourceID, _ := node.Properties["source_id"].(string)
	return sourceID
}

func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = true
	}
	return out
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func clampPriority(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func propertyFloat(props map[string]any, key string, fallback float64) float64 {
	switch v := props[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return fallback
	}
}

func ptrBool(v bool) *bool {
	return &v
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && len(substr) > 0 &&
			func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}())
}
