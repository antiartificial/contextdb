package observe

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// BeliefAudit is the full evidence trail for why the system believes a claim.
type BeliefAudit struct {
	// Node is the claim being audited.
	Node core.Node

	// Source is the original source that wrote this claim.
	Source *core.Source

	// Supporters are nodes connected via "supports" edges.
	Supporters []EvidenceNode

	// Contradictors are nodes connected via "contradicts" edges.
	Contradictors []EvidenceNode

	// ProvenanceChain is the derives_from chain back to the original source.
	ProvenanceChain []EvidenceNode

	// ConfidenceHistory tracks how confidence changed over time.
	ConfidenceHistory []ConfidencePoint

	// AuditedAt is when this audit was generated.
	AuditedAt time.Time
}

// EvidenceNode is a node with its relationship to the audited claim.
type EvidenceNode struct {
	Node     core.Node
	Edge     core.Edge
	Relation string // "supports", "contradicts", "derives_from"
}

// ConfidencePoint is a point in the confidence time-series.
type ConfidencePoint struct {
	Time       time.Time
	Confidence float64
	Version    uint64
}

// AuditBelief generates a full evidence audit for the given node.
func AuditBelief(ctx context.Context, graph store.GraphStore, ns string, nodeID uuid.UUID) (*BeliefAudit, error) {
	node, err := graph.GetNode(ctx, ns, nodeID)
	if err != nil || node == nil {
		return nil, err
	}

	audit := &BeliefAudit{
		Node:      *node,
		AuditedAt: time.Now(),
	}

	// Look up source
	if sourceID, ok := node.Properties["source_id"].(string); ok && sourceID != "" {
		src, err := graph.GetSourceByExternalID(ctx, ns, sourceID)
		if err == nil && src != nil {
			audit.Source = src
		}
	}

	// Find supporters
	supportEdges, err := graph.EdgesTo(ctx, ns, nodeID, []string{core.EdgeSupports})
	if err == nil {
		for _, e := range supportEdges {
			n, err := graph.GetNode(ctx, ns, e.Src)
			if err == nil && n != nil {
				audit.Supporters = append(audit.Supporters, EvidenceNode{
					Node: *n, Edge: e, Relation: core.EdgeSupports,
				})
			}
		}
	}

	// Find contradictors (both directions)
	contradictEdges, err := graph.EdgesFrom(ctx, ns, nodeID, []string{core.EdgeContradicts})
	if err == nil {
		for _, e := range contradictEdges {
			n, err := graph.GetNode(ctx, ns, e.Dst)
			if err == nil && n != nil {
				audit.Contradictors = append(audit.Contradictors, EvidenceNode{
					Node: *n, Edge: e, Relation: core.EdgeContradicts,
				})
			}
		}
	}
	contraEdgesTo, err := graph.EdgesTo(ctx, ns, nodeID, []string{core.EdgeContradicts})
	if err == nil {
		for _, e := range contraEdgesTo {
			n, err := graph.GetNode(ctx, ns, e.Src)
			if err == nil && n != nil {
				audit.Contradictors = append(audit.Contradictors, EvidenceNode{
					Node: *n, Edge: e, Relation: core.EdgeContradicts,
				})
			}
		}
	}

	// Walk provenance chain (derives_from)
	currentID := nodeID
	visited := map[uuid.UUID]bool{nodeID: true}
	for i := 0; i < 10; i++ {
		edges, err := graph.EdgesTo(ctx, ns, currentID, []string{core.EdgeDerivedFrom})
		if err != nil || len(edges) == 0 {
			break
		}
		e := edges[0]
		if visited[e.Src] {
			break
		}
		visited[e.Src] = true
		n, err := graph.GetNode(ctx, ns, e.Src)
		if err != nil || n == nil {
			break
		}
		audit.ProvenanceChain = append(audit.ProvenanceChain, EvidenceNode{
			Node: *n, Edge: e, Relation: core.EdgeDerivedFrom,
		})
		currentID = e.Src
	}

	// Confidence history from version history
	versions, err := graph.History(ctx, ns, nodeID)
	if err == nil {
		for _, v := range versions {
			audit.ConfidenceHistory = append(audit.ConfidenceHistory, ConfidencePoint{
				Time:       v.TxTime,
				Confidence: v.Confidence,
				Version:    v.Version,
			})
		}
	}

	return audit, nil
}
