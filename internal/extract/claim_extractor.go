package extract

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/antiartificial/contextdb/internal/core"
)

// ClaimField maps a named field to a node property or label.
type ClaimField struct {
	Name     string // field name in the input map
	Property string // target property name on the node (empty = use Name)
	Label    bool   // if true, the value is added as a label instead of a property
}

// ClaimSchema defines how structured input maps to graph nodes.
type ClaimSchema struct {
	// ContentField is the input field that becomes the node's main text content.
	ContentField string

	// Fields maps additional input fields to node properties or labels.
	Fields []ClaimField

	// DefaultConfidence is the confidence assigned to extracted nodes.
	// Zero defaults to 0.7.
	DefaultConfidence float64

	// Relations defines edges to create between extracted nodes.
	// Each relation references fields by name to identify subject and object.
	Relations []RelationSchema
}

// RelationSchema defines an edge to create between nodes in the same batch.
type RelationSchema struct {
	SubjectField string  // input field identifying the subject node
	Predicate    string  // edge type (e.g., "supports", "derives_from")
	ObjectField  string  // input field identifying the object node
	Weight       float64 // edge weight; 0 defaults to 0.8
}

// ClaimExtractor decomposes structured input into nodes and edges
// according to a ClaimSchema, without requiring an LLM.
type ClaimExtractor struct {
	Schema ClaimSchema
}

// NewClaimExtractor creates an extractor from the given schema.
func NewClaimExtractor(schema ClaimSchema) *ClaimExtractor {
	if schema.DefaultConfidence == 0 {
		schema.DefaultConfidence = 0.7
	}
	return &ClaimExtractor{Schema: schema}
}

// ExtractFields decomposes a structured input map into nodes and edges.
// The fields map supplies key-value pairs that the schema maps to node
// properties and labels. The namespace and caller-supplied labels come
// from the ExtractionRequest.
func (e *ClaimExtractor) ExtractFields(ctx context.Context, req ExtractionRequest, fields map[string]string) (*ExtractionResult, error) {
	now := time.Now()
	var nodes []core.Node
	var edges []core.Edge

	// Track nodes by field name for relation building.
	nodeByField := make(map[string]uuid.UUID)

	// Build the primary node from the content field.
	content := fields[e.Schema.ContentField]
	if content == "" {
		return &ExtractionResult{}, nil
	}

	nodeID := uuid.New()
	node := core.Node{
		ID:         nodeID,
		Namespace:  req.Namespace,
		Labels:     append([]string{}, req.Labels...),
		Properties: map[string]any{"content": content},
		ValidFrom:  now,
		TxTime:     now,
		Confidence: e.Schema.DefaultConfidence,
	}
	nodeByField[e.Schema.ContentField] = nodeID

	// Apply schema fields.
	for _, f := range e.Schema.Fields {
		val, ok := fields[f.Name]
		if !ok || val == "" {
			continue
		}
		if f.Label {
			node.Labels = append(node.Labels, val)
		} else {
			prop := f.Property
			if prop == "" {
				prop = f.Name
			}
			node.Properties[prop] = val
		}
	}

	nodes = append(nodes, node)

	// Build edges from relation schemas.
	for _, rel := range e.Schema.Relations {
		srcID, srcOK := nodeByField[rel.SubjectField]
		dstID, dstOK := nodeByField[rel.ObjectField]
		if !srcOK || !dstOK {
			continue
		}
		weight := rel.Weight
		if weight == 0 {
			weight = 0.8
		}
		edges = append(edges, core.Edge{
			ID:        uuid.New(),
			Namespace: req.Namespace,
			Src:       srcID,
			Dst:       dstID,
			Type:      rel.Predicate,
			Weight:    weight,
			ValidFrom: now,
			TxTime:    now,
		})
	}

	return &ExtractionResult{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// Extract implements the Extractor interface. It treats req.Text as the
// content field value. For richer structured input, use ExtractFields directly.
func (e *ClaimExtractor) Extract(ctx context.Context, req ExtractionRequest) (*ExtractionResult, error) {
	fields := map[string]string{
		e.Schema.ContentField: req.Text,
	}
	return e.ExtractFields(ctx, req, fields)
}
