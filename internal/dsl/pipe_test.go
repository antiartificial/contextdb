package dsl

import (
	"testing"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
}

func parsePipeWithClock(input string) (*Query, error) {
	tokens := NewLexer(input).All()
	p := &pipeParser{parser: newParser(tokens)}
	p.now = fixedNow
	return p.parse()
}

func TestPipeBasicSearch(t *testing.T) {
	q, err := ParsePipe(`search "project deadlines"`)
	if err != nil {
		t.Fatal(err)
	}
	if q.SearchText != "project deadlines" {
		t.Errorf("SearchText = %q", q.SearchText)
	}
}

func TestPipeFullPipeline(t *testing.T) {
	q, err := parsePipeWithClock(
		`search "project deadlines" | where confidence > 0.7 | weight recency:high | top 10`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if q.SearchText != "project deadlines" {
		t.Errorf("SearchText = %q", q.SearchText)
	}
	if len(q.Predicates) != 1 {
		t.Fatalf("got %d predicates, want 1", len(q.Predicates))
	}
	if q.Predicates[0].Field != "confidence" {
		t.Errorf("predicate field = %q", q.Predicates[0].Field)
	}
	if q.Predicates[0].Op != OpGt {
		t.Errorf("predicate op = %v", q.Predicates[0].Op)
	}
	if q.Predicates[0].Value.Num != 0.7 {
		t.Errorf("predicate value = %v", q.Predicates[0].Value.Num)
	}
	if q.Weights.Recency != 0.8 {
		t.Errorf("recency weight = %v, want 0.8", q.Weights.Recency)
	}
	if q.Limit != 10 {
		t.Errorf("limit = %d", q.Limit)
	}
}

func TestPipeRelativeTime(t *testing.T) {
	q, err := parsePipeWithClock(
		`search "test" | where valid_time > 7 d ago`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Predicates) != 1 {
		t.Fatalf("got %d predicates", len(q.Predicates))
	}
	expected := fixedNow().Add(-7 * 24 * time.Hour)
	got := q.Predicates[0].Value.Time
	if got.Sub(expected) > time.Second {
		t.Errorf("time = %v, want ~%v", got, expected)
	}
}

func TestPipeNamespaceAndMode(t *testing.T) {
	q, err := ParsePipe(`search "test" | in agent_memory mode agent_memory`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Namespace != "agent_memory" {
		t.Errorf("namespace = %q", q.Namespace)
	}
	if q.Mode != "agent_memory" {
		t.Errorf("mode = %q", q.Mode)
	}
}

func TestPipeExpandWithDepth(t *testing.T) {
	q, err := ParsePipe(`search "test" | expand contradicts depth 2`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Graph == nil {
		t.Fatal("graph is nil")
	}
	if len(q.Graph.Edges) != 1 {
		t.Fatalf("got %d edges", len(q.Graph.Edges))
	}
	if q.Graph.Edges[0].Type != "contradicts" {
		t.Errorf("edge type = %q", q.Graph.Edges[0].Type)
	}
	if q.Graph.Edges[0].MaxDepth != 2 {
		t.Errorf("depth = %d", q.Graph.Edges[0].MaxDepth)
	}
}

func TestPipeRerank(t *testing.T) {
	q, err := ParsePipe(`search "test" | rerank`)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Rerank {
		t.Error("expected rerank=true")
	}
}

func TestPipeAsOfAndKnownAt(t *testing.T) {
	// Test underscore form (as_of / known_at)
	q, err := ParsePipe(`search "test" | as_of "2024-06-01" | known_at "2024-06-15"`)
	if err != nil {
		t.Fatal(err)
	}
	if q.ValidAt == nil {
		t.Fatal("ValidAt is nil")
	}
	if q.ValidAt.Year() != 2024 || q.ValidAt.Month() != 6 || q.ValidAt.Day() != 1 {
		t.Errorf("ValidAt = %v", q.ValidAt)
	}
	if q.KnownAt == nil {
		t.Fatal("KnownAt is nil")
	}
	if q.KnownAt.Day() != 15 {
		t.Errorf("KnownAt = %v", q.KnownAt)
	}
}

func TestPipeWeightPresets(t *testing.T) {
	q, err := ParsePipe(`search "test" | weight similarity:high, confidence:low, recency:off, utility:medium`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Weights.Similarity != 0.8 {
		t.Errorf("similarity = %v", q.Weights.Similarity)
	}
	if q.Weights.Confidence != 0.2 {
		t.Errorf("confidence = %v", q.Weights.Confidence)
	}
	if q.Weights.Recency != 0.0 {
		t.Errorf("recency = %v", q.Weights.Recency)
	}
	if q.Weights.Utility != 0.5 {
		t.Errorf("utility = %v", q.Weights.Utility)
	}
}

func TestPipeReturnFields(t *testing.T) {
	q, err := ParsePipe(`search "test" | return content, score, source.name`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Return) != 3 {
		t.Fatalf("got %d return fields", len(q.Return))
	}
	if q.Return[2] != "source.name" {
		t.Errorf("return[2] = %q", q.Return[2])
	}
}

func TestPipeErrorHint(t *testing.T) {
	_, err := ParsePipe(`search "test" | wher confidence > 0.7`)
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T", err)
	}
	if pe.Hint == "" {
		t.Error("expected hint for typo")
	}
}

func TestPipeWhereBetween(t *testing.T) {
	q, err := parsePipeWithClock(
		`search "test" | where confidence between 0.5 and 1.0`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Predicates) != 1 {
		t.Fatalf("got %d predicates", len(q.Predicates))
	}
	if q.Predicates[0].Op != OpBetween {
		t.Errorf("op = %v", q.Predicates[0].Op)
	}
	if q.Predicates[0].Value.Num != 0.5 {
		t.Errorf("lower = %v", q.Predicates[0].Value.Num)
	}
	if q.Predicates[0].UpperBound.Num != 1.0 {
		t.Errorf("upper = %v", q.Predicates[0].UpperBound.Num)
	}
}

func TestPipeMultipleWherePredicates(t *testing.T) {
	q, err := parsePipeWithClock(
		`search "test" | where confidence > 0.5 and source.credibility >= 0.8`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Predicates) != 2 {
		t.Fatalf("got %d predicates, want 2", len(q.Predicates))
	}
}

func TestPipeFullExample(t *testing.T) {
	q, err := parsePipeWithClock(
		`search "Go routing" | where confidence > 0.7 | expand contradicts depth 2 | top 5 | rerank`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if q.SearchText != "Go routing" {
		t.Errorf("SearchText = %q", q.SearchText)
	}
	if q.Limit != 5 {
		t.Errorf("Limit = %d", q.Limit)
	}
	if !q.Rerank {
		t.Error("expected rerank")
	}
	if q.Graph == nil || len(q.Graph.Edges) != 1 {
		t.Error("expected graph traversal")
	}
}
