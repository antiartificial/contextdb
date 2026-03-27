package dsl

import (
	"testing"
)

func TestLexerBasicPipe(t *testing.T) {
	input := `search "hello world" | where confidence > 0.7 | top 10`
	tokens := NewLexer(input).All()

	expected := []struct {
		typ TokenType
		lit string
	}{
		{TokSearch, "search"},
		{TokString, "hello world"},
		{TokPipe, "|"},
		{TokWhere, "where"},
		{TokIdent, "confidence"},
		{TokGt, ">"},
		{TokNumber, "0.7"},
		{TokPipe, "|"},
		{TokTop, "top"},
		{TokInteger, "10"},
		{TokEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}
	for i, want := range expected {
		got := tokens[i]
		if got.Type != want.typ {
			t.Errorf("token[%d]: type = %v, want %v (lit=%q)", i, got.Type, want.typ, got.Lit)
		}
		if got.Lit != want.lit {
			t.Errorf("token[%d]: lit = %q, want %q", i, got.Lit, want.lit)
		}
	}
}

func TestLexerDottedPath(t *testing.T) {
	tokens := NewLexer(`source.credibility >= 0.5`).All()
	if tokens[0].Type != TokIdent || tokens[0].Lit != "source.credibility" {
		t.Errorf("expected dotted ident, got %v %q", tokens[0].Type, tokens[0].Lit)
	}
}

func TestLexerCQLKeywords(t *testing.T) {
	input := `FIND "test" IN NAMESPACE foo WHERE x > 1 LIMIT 5`
	tokens := NewLexer(input).All()

	// FIND, STRING, IN, NAMESPACE, IDENT, WHERE, IDENT, >, INT, LIMIT, INT, EOF
	if tokens[0].Type != TokFind {
		t.Errorf("expected FIND, got %v", tokens[0].Type)
	}
	if tokens[2].Type != TokIn {
		t.Errorf("expected IN, got %v", tokens[2].Type)
	}
	if tokens[3].Type != TokNamespace {
		t.Errorf("expected NAMESPACE, got %v", tokens[3].Type)
	}
}

func TestLexerNegativeNumber(t *testing.T) {
	tokens := NewLexer(`-3.14`).All()
	if tokens[0].Type != TokNumber || tokens[0].Lit != "-3.14" {
		t.Errorf("expected NUMBER -3.14, got %v %q", tokens[0].Type, tokens[0].Lit)
	}
}

func TestLexerStringEscape(t *testing.T) {
	tokens := NewLexer(`"hello \"world\""`).All()
	if tokens[0].Type != TokString || tokens[0].Lit != `hello "world"` {
		t.Errorf("expected escaped string, got %v %q", tokens[0].Type, tokens[0].Lit)
	}
}

func TestLexerPositionTracking(t *testing.T) {
	tokens := NewLexer("search\n  \"test\"").All()
	// "test" should be on line 2
	if tokens[1].Pos.Line != 2 {
		t.Errorf("expected line 2, got %d", tokens[1].Pos.Line)
	}
}
