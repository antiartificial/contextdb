package observe_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/observe"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

const auditNS = "audit-test"

func TestAuditBelief_FullEvidenceChain(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	now := time.Now()

	// Create a source
	src := core.DefaultSource(auditNS, "agent-007")
	is.NoErr(graph.UpsertSource(ctx, src))

	// Create the claim node with a reference to the source
	claimID := uuid.New()
	claim := core.Node{
		ID:        claimID,
		Namespace: auditNS,
		Labels:    []string{"Claim"},
		Properties: map[string]any{
			"text":      "the sky is blue",
			"source_id": src.ExternalID,
		},
		Confidence:    0.9,
		EpistemicType: core.EpistemicAssertion,
		ValidFrom:     now,
	}
	is.NoErr(graph.UpsertNode(ctx, claim))

	// Create a supporter node
	supporterID := uuid.New()
	supporter := core.Node{
		ID:        supporterID,
		Namespace: auditNS,
		Labels:    []string{"Evidence"},
		Properties: map[string]any{
			"text": "light scatters at 450nm wavelength",
		},
		Confidence: 0.95,
		ValidFrom:  now,
	}
	is.NoErr(graph.UpsertNode(ctx, supporter))

	// Create a contradictor node
	contradictorID := uuid.New()
	contradictor := core.Node{
		ID:        contradictorID,
		Namespace: auditNS,
		Labels:    []string{"Claim"},
		Properties: map[string]any{
			"text": "the sky is green",
		},
		Confidence: 0.1,
		ValidFrom:  now,
	}
	is.NoErr(graph.UpsertNode(ctx, contradictor))

	// supporter -[supports]-> claim
	supportEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: auditNS,
		Src:       supporterID,
		Dst:       claimID,
		Type:      core.EdgeSupports,
		Weight:    1.0,
		ValidFrom: now,
	}
	is.NoErr(graph.UpsertEdge(ctx, supportEdge))

	// contradictor -[contradicts]-> claim
	contradictEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: auditNS,
		Src:       contradictorID,
		Dst:       claimID,
		Type:      core.EdgeContradicts,
		Weight:    1.0,
		ValidFrom: now,
	}
	is.NoErr(graph.UpsertEdge(ctx, contradictEdge))

	// Run the audit
	audit, err := observe.AuditBelief(ctx, graph, auditNS, claimID)
	is.NoErr(err)
	is.True(audit != nil)

	// Node is correct
	is.Equal(claimID, audit.Node.ID)
	is.Equal("the sky is blue", audit.Node.Properties["text"])

	// Source is found
	is.True(audit.Source != nil)
	is.Equal(src.ExternalID, audit.Source.ExternalID)

	// 1 supporter
	is.Equal(1, len(audit.Supporters))
	is.Equal(supporterID, audit.Supporters[0].Node.ID)
	is.Equal(core.EdgeSupports, audit.Supporters[0].Relation)

	// 1 contradictor (arrived via EdgesTo — contradictor points at claim)
	is.Equal(1, len(audit.Contradictors))
	is.Equal(contradictorID, audit.Contradictors[0].Node.ID)
	is.Equal(core.EdgeContradicts, audit.Contradictors[0].Relation)

	// Confidence history (at least one entry from the upsert)
	is.True(len(audit.ConfidenceHistory) >= 1)
	is.Equal(0.9, audit.ConfidenceHistory[0].Confidence)

	// AuditedAt is recent
	is.True(!audit.AuditedAt.IsZero())
	is.True(audit.AuditedAt.After(now.Add(-time.Second)))

	t.Logf("audit: supporters=%d contradictors=%d provenance=%d history=%d",
		len(audit.Supporters), len(audit.Contradictors),
		len(audit.ProvenanceChain), len(audit.ConfidenceHistory))
}

func TestAuditBelief_ProvenanceChain(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	now := time.Now()

	// Create an original source node
	originID := uuid.New()
	origin := core.Node{
		ID:        originID,
		Namespace: auditNS,
		Labels:    []string{"Claim"},
		Properties: map[string]any{"text": "raw observation"},
		Confidence: 0.7,
		ValidFrom:  now,
	}
	is.NoErr(graph.UpsertNode(ctx, origin))

	// Create derived node
	derivedID := uuid.New()
	derived := core.Node{
		ID:        derivedID,
		Namespace: auditNS,
		Labels:    []string{"Inference"},
		Properties: map[string]any{"text": "inferred conclusion"},
		Confidence: 0.6,
		ValidFrom:  now,
	}
	is.NoErr(graph.UpsertNode(ctx, derived))

	// origin -[derived_from]-> derived  (origin was derived from derived... or more correctly:
	// derived is derived from origin: origin -[derived_from]-> derived means origin→derived)
	// The interface is: EdgesTo(derived, derived_from) returns edges where Dst=derived
	// so we need: Src=origin, Dst=derived, Type=derived_from
	provEdge := core.Edge{
		ID:        uuid.New(),
		Namespace: auditNS,
		Src:       originID,
		Dst:       derivedID,
		Type:      core.EdgeDerivedFrom,
		Weight:    1.0,
		ValidFrom: now,
	}
	is.NoErr(graph.UpsertEdge(ctx, provEdge))

	audit, err := observe.AuditBelief(ctx, graph, auditNS, derivedID)
	is.NoErr(err)
	is.True(audit != nil)
	is.Equal(derivedID, audit.Node.ID)

	// Provenance chain should contain the origin node
	is.Equal(1, len(audit.ProvenanceChain))
	is.Equal(originID, audit.ProvenanceChain[0].Node.ID)
	is.Equal(core.EdgeDerivedFrom, audit.ProvenanceChain[0].Relation)
}

func TestAuditBelief_NonexistentNodeReturnsNil(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	missingID := uuid.New()
	audit, err := observe.AuditBelief(ctx, graph, auditNS, missingID)
	is.NoErr(err)
	is.True(audit == nil)
}

func TestAuditBelief_NoSourceProperty(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	now := time.Now()

	// Node with no source_id in properties
	nodeID := uuid.New()
	n := core.Node{
		ID:        nodeID,
		Namespace: auditNS,
		Labels:    []string{"Claim"},
		Properties: map[string]any{
			"text": "unsourced claim",
		},
		Confidence: 0.5,
		ValidFrom:  now,
	}
	is.NoErr(graph.UpsertNode(ctx, n))

	audit, err := observe.AuditBelief(ctx, graph, auditNS, nodeID)
	is.NoErr(err)
	is.True(audit != nil)
	is.Equal(nodeID, audit.Node.ID)
	// No source found — should be nil, not an error
	is.True(audit.Source == nil)
	is.Equal(0, len(audit.Supporters))
	is.Equal(0, len(audit.Contradictors))
	is.Equal(0, len(audit.ProvenanceChain))
}
