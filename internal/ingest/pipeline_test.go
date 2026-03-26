package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/extract"
	"github.com/antiartificial/contextdb/internal/ingest"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

func TestPipeline_IngestWithStaticExtractor(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	log := memstore.NewEventLog()

	// Setup source
	src := core.DefaultSource("test", "user:alice")
	src.Labels = []string{"moderator"}
	is.NoErr(graph.UpsertSource(ctx, src))

	nodeID1 := uuid.New()
	nodeID2 := uuid.New()

	extractor := &extract.StaticExtractor{
		Result: &extract.ExtractionResult{
			Nodes: []core.Node{
				{
					ID: nodeID1, Namespace: "test",
					Labels:     []string{"Concept"},
					Properties: map[string]any{"text": "Go"},
					Vector:     []float32{0.9, 0.1, 0.0, 0.0},
					Confidence: 0.8,
					ValidFrom:  time.Now(),
					TxTime:     time.Now(),
				},
				{
					ID: nodeID2, Namespace: "test",
					Labels:     []string{"Concept"},
					Properties: map[string]any{"text": "Concurrency"},
					Vector:     []float32{0.1, 0.9, 0.0, 0.0},
					Confidence: 0.8,
					ValidFrom:  time.Now(),
					TxTime:     time.Now(),
				},
			},
			Edges: []core.Edge{
				{
					ID: uuid.New(), Namespace: "test",
					Src: nodeID1, Dst: nodeID2,
					Type: "relates_to", Weight: 0.9,
					ValidFrom: time.Now(), TxTime: time.Now(),
				},
			},
			Entities: []extract.Entity{
				{Name: "Go", Type: "Concept"},
				{Name: "Concurrency", Type: "Concept"},
			},
		},
	}

	pipeline := ingest.NewPipeline(extractor, graph, vecs, log, ingest.PipelineConfig{
		AdmitThreshold: 0.1,
	})

	result, err := pipeline.Ingest(ctx, ingest.IngestRequest{
		Text:      "Go has great concurrency support",
		Namespace: "test",
		SourceID:  "user:alice",
		Labels:    []string{"Claim"},
	})
	is.NoErr(err)
	is.Equal(result.NodesWritten, 2)
	is.Equal(result.EdgesWritten, 1)
	is.Equal(result.Rejected, 0)
	is.Equal(len(result.Entities), 2)

	// Verify nodes are persisted
	n1, err := graph.GetNode(ctx, "test", nodeID1)
	is.NoErr(err)
	is.True(n1 != nil)

	n2, err := graph.GetNode(ctx, "test", nodeID2)
	is.NoErr(err)
	is.True(n2 != nil)
}

func TestPipeline_RejectsTrollSource(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	log := memstore.NewEventLog()

	// Setup troll source
	src := core.DefaultSource("test", "user:troll")
	src.Labels = []string{"troll"}
	is.NoErr(graph.UpsertSource(ctx, src))

	extractor := &extract.StaticExtractor{
		Result: &extract.ExtractionResult{
			Nodes: []core.Node{
				{
					ID: uuid.New(), Namespace: "test",
					Labels:     []string{"Claim"},
					Properties: map[string]any{"text": "fake news"},
					Vector:     []float32{0.5, 0.5, 0.0, 0.0},
					Confidence: 0.9,
					ValidFrom:  time.Now(),
					TxTime:     time.Now(),
				},
			},
		},
	}

	pipeline := ingest.NewPipeline(extractor, graph, vecs, log, ingest.PipelineConfig{
		AdmitThreshold: 0.1,
	})

	result, err := pipeline.Ingest(ctx, ingest.IngestRequest{
		Text:      "fake news",
		Namespace: "test",
		SourceID:  "user:troll",
	})
	is.NoErr(err)
	is.Equal(result.NodesWritten, 0)
	is.Equal(result.Rejected, 1)
}
