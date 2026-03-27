package dsl

import (
	"fmt"
	"sort"
	"strings"
)

// ParseError is a syntax error with position and optional hint.
type ParseError struct {
	Pos     Position
	Message string
	Hint    string
}

func (e *ParseError) Error() string {
	s := fmt.Sprintf("parse error at %s: %s", e.Pos, e.Message)
	if e.Hint != "" {
		s += ", " + e.Hint
	}
	return s
}

// errAt creates a ParseError at the given position.
func errAt(pos Position, msg string) *ParseError {
	return &ParseError{Pos: pos, Message: msg}
}

// errUnexpected creates an "unexpected token" error with keyword hint.
func errUnexpected(tok Token) *ParseError {
	e := &ParseError{
		Pos:     tok.Pos,
		Message: fmt.Sprintf("unexpected token %q", tok.Lit),
	}
	if tok.Type == TokIdent {
		if hint := suggestKeyword(tok.Lit); hint != "" {
			e.Hint = fmt.Sprintf("did you mean %q?", hint)
		}
	}
	return e
}

// suggestKeyword uses Levenshtein distance to suggest a keyword fix.
func suggestKeyword(input string) string {
	lower := strings.ToLower(input)
	type candidate struct {
		word string
		dist int
	}
	var candidates []candidate
	for kw := range keywords {
		d := levenshtein(lower, kw)
		if d <= 2 {
			candidates = append(candidates, candidate{kw, d})
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})
	return candidates[0].word
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
