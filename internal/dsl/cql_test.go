package dsl

import (
	"testing"
	"time"
)

func parseCQLWithClock(input string) (*Query, error) {
	tokens := NewLexer(input).All()
	p := &cqlParser{parser: newParser(tokens)}
	p.now = fixedNow
	return p.parse()
}

func TestCQLBasicFind(t *testing.T) {
	q, err := ParseCQL(`FIND "project status"`)
	if err != nil {
		t.Fatal(err)
	}
	if q.SearchText != "project status" {
		t.Errorf("SearchText = %q", q.SearchText)
	}
}

func TestCQLFullQuery(t *testing.T) {
	q, err := parseCQLWithClock(`FIND "project status"
		IN NAMESPACE agent_memory
		WHERE valid_time > 7 d ago
			AND source.credibility >= 0.7
		WEIGHT similarity=0.4, recency=0.4, confidence=0.2
		LIMIT 10
		RERANK`)
	if err != nil {
		t.Fatal(err)
	}

	if q.SearchText != "project status" {
		t.Errorf("SearchText = %q", q.SearchText)
	}
	if q.Namespace != "agent_memory" {
		t.Errorf("Namespace = %q", q.Namespace)
	}
	if len(q.Predicates) != 2 {
		t.Fatalf("got %d predicates", len(q.Predicates))
	}
	if q.Weights.Similarity != 0.4 {
		t.Errorf("similarity = %v", q.Weights.Similarity)
	}
	if q.Weights.Recency != 0.4 {
		t.Errorf("recency = %v", q.Weights.Recency)
	}
	if q.Weights.Confidence != 0.2 {
		t.Errorf("confidence = %v", q.Weights.Confidence)
	}
	if q.Limit != 10 {
		t.Errorf("Limit = %d", q.Limit)
	}
	if !q.Rerank {
		t.Error("expected rerank")
	}
}

func TestCQLTemporalClauses(t *testing.T) {
	q, err := ParseCQL(`FIND "Go routing patterns"
		AS OF "2024-06-01"
		KNOWN AT "2024-06-15"`)
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
	if q.KnownAt.Month() != 6 || q.KnownAt.Day() != 15 {
		t.Errorf("KnownAt = %v", q.KnownAt)
	}
}

func TestCQLFollowClause(t *testing.T) {
	q, err := ParseCQL(`FIND "test" FOLLOW contradicts DEPTH 2`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Graph == nil {
		t.Fatal("graph nil")
	}
	if len(q.Graph.Edges) != 1 {
		t.Fatalf("got %d edges", len(q.Graph.Edges))
	}
	if q.Graph.Edges[0].Type != "contradicts" {
		t.Errorf("type = %q", q.Graph.Edges[0].Type)
	}
	if q.Graph.Edges[0].MaxDepth != 2 {
		t.Errorf("depth = %d", q.Graph.Edges[0].MaxDepth)
	}
}

func TestCQLFollowMultiple(t *testing.T) {
	q, err := ParseCQL(`FIND "test" FOLLOW contradicts DEPTH 2, supports DEPTH 1`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Graph.Edges) != 2 {
		t.Fatalf("got %d edges", len(q.Graph.Edges))
	}
}

func TestCQLReturnClause(t *testing.T) {
	q, err := ParseCQL(`FIND "test" RETURN content, source.name, valid_from, score`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Return) != 4 {
		t.Fatalf("got %d return fields", len(q.Return))
	}
	if q.Return[1] != "source.name" {
		t.Errorf("return[1] = %q", q.Return[1])
	}
}

func TestCQLWhereIN(t *testing.T) {
	q, err := ParseCQL(`FIND "team headcount" WHERE label IN ("hr", "org")`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Predicates) != 1 {
		t.Fatalf("got %d predicates", len(q.Predicates))
	}
	if q.Predicates[0].Op != OpIn {
		t.Errorf("op = %v", q.Predicates[0].Op)
	}
	if len(q.Predicates[0].Value.Strings) != 2 {
		t.Errorf("strings = %v", q.Predicates[0].Value.Strings)
	}
}

func TestCQLWhereBetween(t *testing.T) {
	q, err := parseCQLWithClock(
		`FIND "test" WHERE confidence BETWEEN 0.5 AND 1.0`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if q.Predicates[0].Op != OpBetween {
		t.Errorf("op = %v", q.Predicates[0].Op)
	}
}

func TestCQLWhereIsNull(t *testing.T) {
	q, err := ParseCQL(`FIND "test" WHERE valid_until IS NULL`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Predicates[0].Op != OpIsNull {
		t.Errorf("op = %v", q.Predicates[0].Op)
	}
}

func TestCQLWhereIsNotNull(t *testing.T) {
	q, err := ParseCQL(`FIND "test" WHERE valid_until IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Predicates[0].Op != OpIsNotNull {
		t.Errorf("op = %v", q.Predicates[0].Op)
	}
}

func TestCQLRerankWithHint(t *testing.T) {
	q, err := ParseCQL(`FIND "test" RERANK WITH "cohere-v3"`)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Rerank {
		t.Error("expected rerank")
	}
	if q.RerankHint != "cohere-v3" {
		t.Errorf("hint = %q", q.RerankHint)
	}
}

func TestCQLWeightPresets(t *testing.T) {
	q, err := ParseCQL(`FIND "test" WEIGHT utility=high`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Weights.Utility != 0.8 {
		t.Errorf("utility = %v", q.Weights.Utility)
	}
}

func TestCQLRelativeTime(t *testing.T) {
	q, err := parseCQLWithClock(`FIND "test" WHERE valid_time > 7 d ago`)
	if err != nil {
		t.Fatal(err)
	}
	expected := fixedNow().Add(-7 * 24 * time.Hour)
	got := q.Predicates[0].Value.Time
	if got.Sub(expected) > time.Second {
		t.Errorf("time = %v, want ~%v", got, expected)
	}
}

func TestCQLCaseInsensitive(t *testing.T) {
	// Lowercase CQL should also work
	q, err := ParseCQL(`find "test" where confidence > 0.5 limit 5`)
	if err != nil {
		t.Fatal(err)
	}
	if q.SearchText != "test" {
		t.Errorf("SearchText = %q", q.SearchText)
	}
	if q.Limit != 5 {
		t.Errorf("Limit = %d", q.Limit)
	}
}

func TestCQLFullExample2(t *testing.T) {
	q, err := parseCQLWithClock(`FIND "team headcount"
		WHERE label IN ("hr", "org")
			AND confidence BETWEEN 0.5 AND 1.0
		WEIGHT utility=high
		LIMIT 5`)
	if err != nil {
		t.Fatal(err)
	}
	if q.SearchText != "team headcount" {
		t.Errorf("SearchText = %q", q.SearchText)
	}
	if len(q.Predicates) != 2 {
		t.Fatalf("got %d predicates", len(q.Predicates))
	}
	if q.Weights.Utility != 0.8 {
		t.Errorf("utility = %v", q.Weights.Utility)
	}
	if q.Limit != 5 {
		t.Errorf("Limit = %d", q.Limit)
	}
}

func TestCQLExcludeSources(t *testing.T) {
	q, err := ParseCQL(`FIND "test" EXCLUDE SOURCES "bot-123", "spam-456"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.ExcludeSourceIDs) != 2 {
		t.Fatalf("got %d excluded sources", len(q.ExcludeSourceIDs))
	}
	if q.ExcludeSourceIDs[0] != "bot-123" {
		t.Errorf("source[0] = %q", q.ExcludeSourceIDs[0])
	}
}

func TestCQLErrorOnSingleQuotes(t *testing.T) {
	_, err := ParseCQL(`FIND 'test'`)
	if err == nil {
		t.Error("expected error on single quotes")
	}
}
