package core

import (
	"time"

	"github.com/google/uuid"
)

const (
	EpistemicAssertion   = "assertion"   // stated as fact (e.g., calendar event)
	EpistemicObservation = "observation" // reported/observed (e.g., "I think...")
	EpistemicInference   = "inference"   // derived from other claims
)

// Node is the fundamental storage unit. Schema-free — callers define what
// Labels and Properties mean. The database enforces temporal correctness
// and namespace isolation; it does not interpret content.
type Node struct {
	ID         uuid.UUID
	Namespace  string
	Labels     []string
	Properties map[string]any
	Vector     []float32 // nil if not yet embedded
	ModelID    string    // which embedding model produced Vector

	// bi-temporal
	ValidFrom  time.Time
	ValidUntil *time.Time // nil = currently valid
	TxTime     time.Time  // when the system ingested this

	// epistemic
	Confidence float64 // 0.0–1.0; 0 treated as 0.5 (unknown)
	// EpistemicType classifies the claim's epistemic status.
	// Values: "assertion", "observation", "inference", or "" (untyped).
	// Observations carry less weight in consensus than assertions.
	EpistemicType string
	Version       uint64
}

// IsValidAt reports whether the node represents a currently valid fact
// as of the given time.
func (n Node) IsValidAt(t time.Time) bool {
	if t.Before(n.ValidFrom) {
		return false
	}
	if n.ValidUntil != nil && t.After(*n.ValidUntil) {
		return false
	}
	return true
}

// HasLabel reports whether the node carries the given label.
func (n Node) HasLabel(label string) bool {
	for _, l := range n.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// NodeText extracts the text content from a node's properties.
// Checks "text" first, then "content". Returns "" if neither exists.
func NodeText(n Node) string {
	if t, ok := n.Properties["text"].(string); ok {
		return t
	}
	if t, ok := n.Properties["content"].(string); ok {
		return t
	}
	return ""
}
