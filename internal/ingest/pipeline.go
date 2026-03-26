package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/extract"
	"github.com/antiartificial/contextdb/internal/store"
)

// Pipeline orchestrates: extract → admit → write for raw text ingestion.
type Pipeline struct {
	extractor extract.Extractor
	graph     store.GraphStore
	vecs      store.VectorIndex
	log       store.EventLog
	config    PipelineConfig
}

// PipelineConfig configures the ingest pipeline.
type PipelineConfig struct {
	AdmitThreshold float64 // admission threshold [0, 1]
}

// NewPipeline returns a pipeline that extracts, admits, and writes graph elements.
func NewPipeline(ext extract.Extractor, graph store.GraphStore, vecs store.VectorIndex, log store.EventLog, cfg PipelineConfig) *Pipeline {
	return &Pipeline{
		extractor: ext,
		graph:     graph,
		vecs:      vecs,
		log:       log,
		config:    cfg,
	}
}

// IngestRequest describes a raw text ingestion.
type IngestRequest struct {
	Text      string
	Namespace string
	SourceID  string
	Labels    []string
}

// IngestResult describes the outcome of a pipeline run.
type IngestResult struct {
	NodesWritten int
	EdgesWritten int
	Rejected     int
	Entities     []extract.Entity
}

// Ingest runs raw text through extraction, admission, and persistence.
func (p *Pipeline) Ingest(ctx context.Context, req IngestRequest) (*IngestResult, error) {
	// Step 1: Extract entities and relations
	result, err := p.extractor.Extract(ctx, extract.ExtractionRequest{
		Text:      req.Text,
		Namespace: req.Namespace,
		SourceID:  req.SourceID,
		Labels:    req.Labels,
	})
	if err != nil {
		return nil, fmt.Errorf("extraction: %w", err)
	}

	// Step 2: Resolve source
	src, err := p.graph.GetSourceByExternalID(ctx, req.Namespace, req.SourceID)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	if src == nil {
		defaultSrc := core.DefaultSource(req.Namespace, req.SourceID)
		if err := p.graph.UpsertSource(ctx, defaultSrc); err != nil {
			return nil, fmt.Errorf("create source: %w", err)
		}
		src = &defaultSrc
	}

	// Step 3: Admit and write each node
	var written, rejected int
	nodeIDMap := make(map[uuid.UUID]uuid.UUID) // original ID → written ID

	for _, node := range result.Nodes {
		origID := node.ID

		// Near-duplicate check
		var nearest []core.ScoredNode
		if len(node.Vector) > 0 {
			nearest, err = p.vecs.Search(ctx, store.VectorQuery{
				Namespace: req.Namespace,
				Vector:    node.Vector,
				TopK:      5,
				AsOf:      time.Now(),
			})
			if err != nil {
				return nil, fmt.Errorf("near-dup scan: %w", err)
			}
		}

		decision := Admit(AdmitRequest{
			Candidate:         node,
			Source:            *src,
			NearestNeighbours: nearest,
			Threshold:         p.config.AdmitThreshold,
		})

		if !decision.Admit {
			rejected++
			continue
		}

		node.Confidence *= decision.ConfidenceMultiplier

		if err := p.graph.UpsertNode(ctx, node); err != nil {
			return nil, fmt.Errorf("upsert node: %w", err)
		}

		// Index vector if present
		if len(node.Vector) > 0 {
			nID := node.ID
			if err := p.vecs.Index(ctx, core.VectorEntry{
				ID:        uuid.New(),
				Namespace: req.Namespace,
				NodeID:    &nID,
				Vector:    node.Vector,
				Text:      fmt.Sprintf("%v", node.Properties["text"]),
				CreatedAt: time.Now(),
			}); err != nil {
				return nil, fmt.Errorf("index vector: %w", err)
			}
			// Register node for search assembly if supported
			if reg, ok := p.vecs.(interface{ RegisterNode(core.Node) }); ok {
				reg.RegisterNode(node)
			}
		}

		nodeIDMap[origID] = node.ID
		written++
	}

	// Step 4: Write edges (only between admitted nodes)
	var edgesWritten int
	for _, edge := range result.Edges {
		if _, srcOK := nodeIDMap[edge.Src]; !srcOK {
			continue
		}
		if _, dstOK := nodeIDMap[edge.Dst]; !dstOK {
			continue
		}
		if err := p.graph.UpsertEdge(ctx, edge); err != nil {
			return nil, fmt.Errorf("upsert edge: %w", err)
		}
		edgesWritten++
	}

	// Step 5: Update source claim counters
	// (Actual Bayesian update happens when claims are validated/refuted)
	src.ClaimsAsserted += int64(written)
	if err := p.graph.UpsertSource(ctx, *src); err != nil {
		// Non-fatal: log but don't fail ingestion
		// Source credibility update can happen asynchronously
	}

	return &IngestResult{
		NodesWritten: written,
		EdgesWritten: edgesWritten,
		Rejected:     rejected,
		Entities:     result.Entities,
	}, nil
}
