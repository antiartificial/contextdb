package retrieval

import (
	"testing"

	"github.com/antiartificial/contextdb/internal/core"
)

func TestContrastivePairStructure(t *testing.T) {
	// Verify ContrastivePair can hold a node with nil contradictor
	pair := ContrastivePair{
		Node: core.ScoredNode{
			Node:  core.Node{Confidence: 0.9},
			Score: 0.85,
		},
	}
	if pair.Contradictor != nil {
		t.Error("expected nil contradictor")
	}
	if pair.Node.Score != 0.85 {
		t.Errorf("score = %v", pair.Node.Score)
	}
}
