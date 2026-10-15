package store_test

import (
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/google/uuid"
)

func TestLexicalIdentifierAndPhraseBoundaries(t *testing.T) {
	now := time.Now()
	nodes := []core.Node{
		{ID: uuid.New(), Namespace: "one", Properties: map[string]any{"text": "Invoice INV-8Q7A was approved"}, ValidFrom: now.Add(-time.Hour)},
		{ID: uuid.New(), Namespace: "one", Properties: map[string]any{"text": "Invoice XINV-8Q7A was approved"}, ValidFrom: now.Add(-time.Hour)},
		{ID: uuid.New(), Namespace: "two", Properties: map[string]any{"text": "Invoice INV-8Q7A was approved"}, ValidFrom: now.Add(-time.Hour)},
	}
	q := store.TextQuery{Namespace: "one", Text: "Find INV-8Q7A", ValidAt: now, KnownAt: now}
	got := store.RankTextCandidates(nodes, q)
	if len(got) != 1 || got[0].Node.ID != nodes[0].ID {
		t.Fatalf("exact identifier: %+v", got)
	}
	q.Text = `find "Invoice INV-8Q7A"`
	got = store.RankTextCandidates(nodes, q)
	if len(got) != 1 || got[0].Node.ID != nodes[0].ID {
		t.Fatalf("quoted phrase: %+v", got)
	}
}
