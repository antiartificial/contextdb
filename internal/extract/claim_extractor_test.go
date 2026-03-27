package extract

import (
	"context"
	"testing"
)

func TestClaimExtractor_BasicExtraction(t *testing.T) {
	schema := ClaimSchema{
		ContentField: "content",
		Fields: []ClaimField{
			{Name: "category", Label: true},
			{Name: "source", Property: "source_name"},
		},
		DefaultConfidence: 0.9,
	}
	ext := NewClaimExtractor(schema)

	req := ExtractionRequest{
		Namespace: "test-ns",
		Labels:    []string{"claim"},
	}
	fields := map[string]string{
		"content":  "Go 1.22 was released in February 2024",
		"category": "release",
		"source":   "golang.org",
	}

	result, err := ext.ExtractFields(context.Background(), req, fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}

	node := result.Nodes[0]

	// Check namespace
	if node.Namespace != "test-ns" {
		t.Errorf("namespace = %q, want %q", node.Namespace, "test-ns")
	}

	// Check confidence
	if node.Confidence != 0.9 {
		t.Errorf("confidence = %f, want 0.9", node.Confidence)
	}

	// Check content property
	if got, ok := node.Properties["content"]; !ok || got != "Go 1.22 was released in February 2024" {
		t.Errorf("content property = %v, want the input text", got)
	}

	// Check source_name property (remapped from "source")
	if got, ok := node.Properties["source_name"]; !ok || got != "golang.org" {
		t.Errorf("source_name property = %v, want %q", got, "golang.org")
	}

	// Check labels: should have caller label "claim" + schema label "release"
	wantLabels := map[string]bool{"claim": true, "release": true}
	for _, l := range node.Labels {
		delete(wantLabels, l)
	}
	if len(wantLabels) > 0 {
		t.Errorf("missing labels: %v; got %v", wantLabels, node.Labels)
	}
}

func TestClaimExtractor_EmptyContent(t *testing.T) {
	schema := ClaimSchema{
		ContentField: "content",
	}
	ext := NewClaimExtractor(schema)

	result, err := ext.Extract(context.Background(), ExtractionRequest{
		Text:      "",
		Namespace: "ns",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("expected 0 nodes for empty content, got %d", len(result.Nodes))
	}
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges for empty content, got %d", len(result.Edges))
	}
}

func TestClaimExtractor_ExtractViaInterface(t *testing.T) {
	schema := ClaimSchema{
		ContentField: "content",
	}
	ext := NewClaimExtractor(schema)

	// Verify it satisfies the Extractor interface.
	var _ Extractor = ext

	result, err := ext.Extract(context.Background(), ExtractionRequest{
		Text:      "some claim",
		Namespace: "ns",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	if got := result.Nodes[0].Properties["content"]; got != "some claim" {
		t.Errorf("content = %v, want %q", got, "some claim")
	}
}

func TestClaimExtractor_DefaultConfidence(t *testing.T) {
	schema := ClaimSchema{
		ContentField: "content",
		// DefaultConfidence left at 0, should become 0.7
	}
	ext := NewClaimExtractor(schema)

	result, err := ext.Extract(context.Background(), ExtractionRequest{
		Text:      "a fact",
		Namespace: "ns",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Nodes[0].Confidence != 0.7 {
		t.Errorf("confidence = %f, want 0.7", result.Nodes[0].Confidence)
	}
}
