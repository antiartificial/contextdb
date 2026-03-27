package retrieval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
)

// NarrativeReport is a structured explanation of what the system believes
// about a claim and why.
type NarrativeReport struct {
	NodeID      uuid.UUID
	Namespace   string
	GeneratedAt time.Time

	// Summary is a one-paragraph explanation of the claim's status.
	Summary string

	// Claim is the node being explained.
	Claim CitedClaim

	// Evidence lists supporting claims with citations.
	Evidence []CitedClaim

	// Contradictions lists contradicting claims.
	Contradictions []CitedClaim

	// ProvenanceChain traces where this claim came from.
	Provenance []CitedClaim

	// ConfidenceExplanation describes why the confidence is what it is.
	ConfidenceExplanation string

	// Grounding reports which parts of the explanation are backed by stored data.
	Grounding []GroundingResult
}

// CitedClaim is a claim with full citation metadata.
type CitedClaim struct {
	NodeID          uuid.UUID
	SourceID        string
	Text            string
	Confidence      float64
	EpistemicType   string
	ValidFrom       time.Time
	ValidUntil      *time.Time
	ProvenanceDepth int
	Relation        string // "supports", "contradicts", "derives_from", or "" for the target
}

// narrativeAudit is a self-contained view of the belief graph for a single node,
// assembled without importing the observe package (which would create a cycle).
type narrativeAudit struct {
	node              core.Node
	source            *core.Source
	supporters        []narrativeEvidence
	contradictors     []narrativeEvidence
	provenanceChain   []narrativeEvidence
	confidenceHistory []core.Node // versions oldest→newest
}

type narrativeEvidence struct {
	node     core.Node
	relation string
}

// NarrativeFormatter generates structured explanations from the belief graph.
type NarrativeFormatter struct {
	graph store.GraphStore
	vecs  store.VectorIndex
}

// NewNarrativeFormatter creates a narrative formatter.
func NewNarrativeFormatter(graph store.GraphStore, vecs store.VectorIndex) *NarrativeFormatter {
	return &NarrativeFormatter{graph: graph, vecs: vecs}
}

// Explain generates a full narrative report for a claim.
func (f *NarrativeFormatter) Explain(ctx context.Context, ns string, nodeID uuid.UUID) (*NarrativeReport, error) {
	audit, err := f.buildAudit(ctx, ns, nodeID)
	if err != nil {
		return nil, err
	}
	if audit == nil {
		return nil, nil
	}

	report := &NarrativeReport{
		NodeID:      nodeID,
		Namespace:   ns,
		GeneratedAt: time.Now(),
		Claim:       nodeToCitedClaim(audit.node, ""),
	}

	for _, s := range audit.supporters {
		report.Evidence = append(report.Evidence, nodeToCitedClaim(s.node, s.relation))
	}

	for _, c := range audit.contradictors {
		report.Contradictions = append(report.Contradictions, nodeToCitedClaim(c.node, c.relation))
	}

	for i, p := range audit.provenanceChain {
		cc := nodeToCitedClaim(p.node, p.relation)
		cc.ProvenanceDepth = i + 1
		report.Provenance = append(report.Provenance, cc)
	}

	report.Summary = f.generateSummary(audit)
	report.ConfidenceExplanation = f.explainConfidence(audit)

	return report, nil
}

// buildAudit assembles the belief evidence for a node using only the GraphStore,
// mirroring the logic in observe.AuditBelief without creating an import cycle.
func (f *NarrativeFormatter) buildAudit(ctx context.Context, ns string, nodeID uuid.UUID) (*narrativeAudit, error) {
	node, err := f.graph.GetNode(ctx, ns, nodeID)
	if err != nil || node == nil {
		return nil, err
	}

	audit := &narrativeAudit{node: *node}

	// Look up source.
	if sourceID, ok := node.Properties["source_id"].(string); ok && sourceID != "" {
		src, err := f.graph.GetSourceByExternalID(ctx, ns, sourceID)
		if err == nil && src != nil {
			audit.source = src
		}
	}

	// Find supporters (edges pointing TO this node with type "supports").
	supportEdges, err := f.graph.EdgesTo(ctx, ns, nodeID, []string{core.EdgeSupports})
	if err == nil {
		for _, e := range supportEdges {
			n, err := f.graph.GetNode(ctx, ns, e.Src)
			if err == nil && n != nil {
				audit.supporters = append(audit.supporters, narrativeEvidence{
					node: *n, relation: core.EdgeSupports,
				})
			}
		}
	}

	// Find contradictors (edges FROM this node and edges TO this node with type "contradicts").
	contradictEdges, err := f.graph.EdgesFrom(ctx, ns, nodeID, []string{core.EdgeContradicts})
	if err == nil {
		for _, e := range contradictEdges {
			n, err := f.graph.GetNode(ctx, ns, e.Dst)
			if err == nil && n != nil {
				audit.contradictors = append(audit.contradictors, narrativeEvidence{
					node: *n, relation: core.EdgeContradicts,
				})
			}
		}
	}
	contraEdgesTo, err := f.graph.EdgesTo(ctx, ns, nodeID, []string{core.EdgeContradicts})
	if err == nil {
		for _, e := range contraEdgesTo {
			n, err := f.graph.GetNode(ctx, ns, e.Src)
			if err == nil && n != nil {
				audit.contradictors = append(audit.contradictors, narrativeEvidence{
					node: *n, relation: core.EdgeContradicts,
				})
			}
		}
	}

	// Walk provenance chain (derives_from), up to 10 hops.
	currentID := nodeID
	visited := map[uuid.UUID]bool{nodeID: true}
	for i := 0; i < 10; i++ {
		edges, err := f.graph.EdgesTo(ctx, ns, currentID, []string{core.EdgeDerivedFrom})
		if err != nil || len(edges) == 0 {
			break
		}
		e := edges[0]
		if visited[e.Src] {
			break
		}
		visited[e.Src] = true
		n, err := f.graph.GetNode(ctx, ns, e.Src)
		if err != nil || n == nil {
			break
		}
		audit.provenanceChain = append(audit.provenanceChain, narrativeEvidence{
			node: *n, relation: core.EdgeDerivedFrom,
		})
		currentID = e.Src
	}

	// Confidence history from version history.
	versions, err := f.graph.History(ctx, ns, nodeID)
	if err == nil {
		audit.confidenceHistory = versions
	}

	return audit, nil
}

func nodeToCitedClaim(n core.Node, relation string) CitedClaim {
	cc := CitedClaim{
		NodeID:        n.ID,
		Confidence:    n.Confidence,
		EpistemicType: n.EpistemicType,
		ValidFrom:     n.ValidFrom,
		ValidUntil:    n.ValidUntil,
		Relation:      relation,
	}
	cc.Text = core.NodeText(n)
	if sid, ok := n.Properties["source_id"].(string); ok {
		cc.SourceID = sid
	}
	return cc
}

func (f *NarrativeFormatter) generateSummary(audit *narrativeAudit) string {
	conf := audit.node.Confidence
	if conf == 0 {
		conf = 0.5
	}

	var parts []string

	switch {
	case conf >= 0.9:
		parts = append(parts, "High confidence claim")
	case conf >= 0.7:
		parts = append(parts, "Moderately confident claim")
	case conf >= 0.5:
		parts = append(parts, "Uncertain claim")
	default:
		parts = append(parts, "Low confidence claim")
	}

	if audit.source != nil {
		cred := audit.source.EffectiveCredibility()
		switch {
		case cred >= 0.8:
			parts = append(parts, fmt.Sprintf("from a highly credible source (%.0f%%)", cred*100))
		case cred >= 0.5:
			parts = append(parts, fmt.Sprintf("from a moderately credible source (%.0f%%)", cred*100))
		default:
			parts = append(parts, fmt.Sprintf("from a low-credibility source (%.0f%%)", cred*100))
		}
	}

	if len(audit.supporters) > 0 {
		parts = append(parts, fmt.Sprintf("supported by %d piece(s) of evidence", len(audit.supporters)))
	}

	if len(audit.contradictors) > 0 {
		parts = append(parts, fmt.Sprintf("with %d active contradiction(s)", len(audit.contradictors)))
	}

	if len(audit.provenanceChain) > 0 {
		parts = append(parts, fmt.Sprintf("derived through %d hop(s)", len(audit.provenanceChain)))
	}

	return strings.Join(parts, ", ") + "."
}

func (f *NarrativeFormatter) explainConfidence(audit *narrativeAudit) string {
	conf := audit.node.Confidence
	if conf == 0 {
		conf = 0.5
	}

	var factors []string

	if audit.source != nil {
		factors = append(factors, fmt.Sprintf("source credibility: %.0f%%", audit.source.EffectiveCredibility()*100))
	}

	if len(audit.supporters) > 0 {
		factors = append(factors, fmt.Sprintf("%d supporting claims", len(audit.supporters)))
	}

	if len(audit.contradictors) > 0 {
		factors = append(factors, fmt.Sprintf("%d contradicting claims (reducing confidence)", len(audit.contradictors)))
	}

	if len(audit.confidenceHistory) > 1 {
		first := audit.confidenceHistory[0].Confidence
		last := audit.confidenceHistory[len(audit.confidenceHistory)-1].Confidence
		if last > first {
			factors = append(factors, "confidence has increased over time")
		} else if last < first {
			factors = append(factors, "confidence has decreased over time")
		}
	}

	if len(factors) == 0 {
		return fmt.Sprintf("Confidence %.0f%% based on default assessment.", conf*100)
	}
	return fmt.Sprintf("Confidence %.0f%% based on: %s.", conf*100, strings.Join(factors, "; "))
}
