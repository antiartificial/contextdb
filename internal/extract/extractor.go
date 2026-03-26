// Package extract provides entity/relation extraction from raw text.
// The Extractor interface abstracts the extraction backend (LLM, rules, etc.).
package extract

import (
	"context"

	"github.com/antiartificial/contextdb/internal/core"
)

// Extractor transforms raw text into graph elements.
type Extractor interface {
	Extract(ctx context.Context, req ExtractionRequest) (*ExtractionResult, error)
}

// ExtractionRequest describes what to extract from.
type ExtractionRequest struct {
	Text      string
	Namespace string
	SourceID  string
	Labels    []string // caller-supplied labels to propagate
}

// ExtractionResult holds structured output from an extractor.
type ExtractionResult struct {
	Nodes    []core.Node
	Edges    []core.Edge
	Entities []Entity
}

// Entity is a raw extracted entity before it becomes a Node.
type Entity struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"` // "Person", "Organization", "Concept", etc.
	Properties map[string]any `json:"properties,omitempty"`
}

// Relation is a raw extracted relation before it becomes an Edge.
type Relation struct {
	Subject   string  `json:"subject"`   // entity name
	Predicate string  `json:"predicate"` // edge type
	Object    string  `json:"object"`    // entity name
	Weight    float64 `json:"weight,omitempty"`
}
