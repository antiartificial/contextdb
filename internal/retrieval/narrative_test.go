package retrieval_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/retrieval"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

const narrativeNS = "test:narrative"

func TestExplain_WithSupporterAndContradiction(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	now := time.Now()

	// Create the main claim node.
	claimNode := core.Node{
		ID:            uuid.New(),
		Namespace:     narrativeNS,
		Labels:        []string{"Claim"},
		Properties:    map[string]any{"text": "the sky is blue", "source_id": "src-42"},
		Confidence:    0.8,
		EpistemicType: "assertion",
		ValidFrom:     now,
		TxTime:        now,
	}
	is.NoErr(graph.UpsertNode(ctx, claimNode))

	// Create a supporting node.
	supporter := core.Node{
		ID:         uuid.New(),
		Namespace:  narrativeNS,
		Labels:     []string{"Evidence"},
		Properties: map[string]any{"text": "spectroscopic measurements confirm blue wavelength"},
		Confidence: 0.9,
		ValidFrom:  now,
		TxTime:     now,
	}
	is.NoErr(graph.UpsertNode(ctx, supporter))

	// Create a contradicting node.
	contradictor := core.Node{
		ID:         uuid.New(),
		Namespace:  narrativeNS,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "the sky is green"},
		Confidence: 0.2,
		ValidFrom:  now,
		TxTime:     now,
	}
	is.NoErr(graph.UpsertNode(ctx, contradictor))

	// supporter --supports--> claimNode
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: narrativeNS,
		Src:       supporter.ID,
		Dst:       claimNode.ID,
		Type:      core.EdgeSupports,
		Weight:    0.9,
		ValidFrom: now,
		TxTime:    now,
	}))

	// claimNode --contradicts--> contradictor
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: narrativeNS,
		Src:       claimNode.ID,
		Dst:       contradictor.ID,
		Type:      core.EdgeContradicts,
		Weight:    1.0,
		ValidFrom: now,
		TxTime:    now,
	}))

	formatter := retrieval.NewNarrativeFormatter(graph, nil)
	report, err := formatter.Explain(ctx, narrativeNS, claimNode.ID)
	is.NoErr(err)
	is.True(report != nil)

	// NodeID and Namespace are set correctly.
	is.Equal(report.NodeID, claimNode.ID)
	is.Equal(report.Namespace, narrativeNS)

	// Summary mentions supporter and contradiction counts.
	is.True(strings.Contains(report.Summary, "supported by 1 piece(s)"))
	is.True(strings.Contains(report.Summary, "1 active contradiction(s)"))

	// CitedClaim fields for the main claim.
	is.Equal(report.Claim.NodeID, claimNode.ID)
	is.Equal(report.Claim.Text, "the sky is blue")
	is.Equal(report.Claim.SourceID, "src-42")
	is.Equal(report.Claim.Confidence, 0.8)
	is.Equal(report.Claim.EpistemicType, "assertion")
	is.Equal(report.Claim.Relation, "") // target has no relation label

	// Evidence list has exactly one entry pointing to the supporter.
	is.Equal(len(report.Evidence), 1)
	is.Equal(report.Evidence[0].NodeID, supporter.ID)
	is.Equal(report.Evidence[0].Text, "spectroscopic measurements confirm blue wavelength")
	is.Equal(report.Evidence[0].Relation, core.EdgeSupports)

	// Contradictions list has exactly one entry.
	is.Equal(len(report.Contradictions), 1)
	is.Equal(report.Contradictions[0].NodeID, contradictor.ID)
	is.Equal(report.Contradictions[0].Relation, core.EdgeContradicts)

	// ConfidenceExplanation is non-empty.
	is.True(len(report.ConfidenceExplanation) > 0)
}

func TestExplain_ProvenanceChain(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	now := time.Now()

	// Build: derived --derives_from--> parent --derives_from--> root
	// AuditBelief walks EdgesTo(derived, derives_from), which finds edges where
	// Dst == derived. So we model: parent derives_from derived (parent is older,
	// derived is newer and references parent as provenance source).
	// Looking at audit.go: EdgesTo(currentID, derives_from) returns edges where
	// Dst == currentID, then follows e.Src. So the structure is:
	// parent.Src --derives_from--> currentID.Dst
	// i.e., parent IS the Src, derived claim IS the Dst.

	derived := core.Node{
		ID:         uuid.New(),
		Namespace:  narrativeNS,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "derived claim"},
		Confidence: 0.7,
		ValidFrom:  now,
		TxTime:     now,
	}
	parent := core.Node{
		ID:         uuid.New(),
		Namespace:  narrativeNS,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "parent claim"},
		Confidence: 0.8,
		ValidFrom:  now,
		TxTime:     now,
	}

	is.NoErr(graph.UpsertNode(ctx, derived))
	is.NoErr(graph.UpsertNode(ctx, parent))

	// parent --derives_from--> derived  (parent is the provenance source)
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: narrativeNS,
		Src:       parent.ID,
		Dst:       derived.ID,
		Type:      core.EdgeDerivedFrom,
		Weight:    1.0,
		ValidFrom: now,
		TxTime:    now,
	}))

	formatter := retrieval.NewNarrativeFormatter(graph, nil)
	report, err := formatter.Explain(ctx, narrativeNS, derived.ID)
	is.NoErr(err)
	is.True(report != nil)

	// Provenance chain should have one hop.
	is.Equal(len(report.Provenance), 1)
	is.Equal(report.Provenance[0].NodeID, parent.ID)
	is.Equal(report.Provenance[0].ProvenanceDepth, 1)
	is.Equal(report.Provenance[0].Relation, core.EdgeDerivedFrom)

	// Summary should mention the derived hop.
	is.True(strings.Contains(report.Summary, "derived through 1 hop(s)"))
}

func TestExplain_NonexistentNode_ReturnsNil(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	formatter := retrieval.NewNarrativeFormatter(graph, nil)
	report, err := formatter.Explain(ctx, narrativeNS, uuid.New())
	is.NoErr(err)
	is.True(report == nil)
}

func TestExplain_TextFromContentProperty(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	now := time.Now()

	// Node uses "content" key instead of "text".
	node := core.Node{
		ID:         uuid.New(),
		Namespace:  narrativeNS,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"content": "alternative text field"},
		Confidence: 0.6,
		ValidFrom:  now,
		TxTime:     now,
	}
	is.NoErr(graph.UpsertNode(ctx, node))

	formatter := retrieval.NewNarrativeFormatter(graph, nil)
	report, err := formatter.Explain(ctx, narrativeNS, node.ID)
	is.NoErr(err)
	is.True(report != nil)
	is.Equal(report.Claim.Text, "alternative text field")
}

func TestExplain_LowConfidenceSummary(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()

	now := time.Now()

	node := core.Node{
		ID:         uuid.New(),
		Namespace:  narrativeNS,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "low confidence claim"},
		Confidence: 0.3,
		ValidFrom:  now,
		TxTime:     now,
	}
	is.NoErr(graph.UpsertNode(ctx, node))

	formatter := retrieval.NewNarrativeFormatter(graph, nil)
	report, err := formatter.Explain(ctx, narrativeNS, node.ID)
	is.NoErr(err)
	is.True(report != nil)
	is.True(strings.Contains(report.Summary, "Low confidence claim"))
	is.True(strings.Contains(report.ConfidenceExplanation, "30%"))
}
