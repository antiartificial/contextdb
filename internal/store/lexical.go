package store

import (
	"sort"
	"strings"
	"unicode"

	"github.com/antiartificial/contextdb/internal/core"
)

// RankTextCandidates keeps lexical matching identical across storage engines.
// An exact phrase or identifier outranks scattered word matches. Storage
// engines may replace their candidate scan with an index without changing it.
func RankTextCandidates(nodes []core.Node, q TextQuery) []core.ScoredNode {
	terms := lexicalTerms(q.Text)
	if len(terms) == 0 {
		return nil
	}
	identifiers := QueryIdentifiers(q.Text)
	phrases := QuotedPhrases(q.Text)
	phrase := strings.ToLower(strings.TrimSpace(q.Text))
	var out []core.ScoredNode
	for _, n := range nodes {
		if n.Namespace != q.Namespace {
			continue
		}
		if !n.IsValidAt(q.ValidAt) {
			continue
		}
		matches := true
		for _, label := range q.Labels {
			if !n.HasLabel(label) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		body := strings.ToLower(core.NodeText(n))
		if body == "" {
			continue
		}
		if !MatchesIdentifiers(body, identifiers) {
			continue
		}
		phraseMatch := true
		for _, quoted := range phrases {
			if !strings.Contains(body, quoted) {
				phraseMatch = false
				break
			}
		}
		if !phraseMatch {
			continue
		}
		found := 0
		for _, term := range terms {
			if strings.Contains(body, term) {
				found++
			}
		}
		if found == 0 {
			continue
		}
		score := float64(found) / float64(len(terms)) * 0.7
		if strings.Contains(body, phrase) {
			score = 1
		}
		// Identifier matches survive surrounding natural-language words.
		for _, term := range identifiers {
			if strings.Contains(body, term) {
				score = 1
				break
			}
		}
		out = append(out, core.ScoredNode{Node: n, Score: score, SimilarityScore: score, RetrievalSource: "lexical"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Node.ID.String() < out[j].Node.ID.String()
		}
		return out[i].Score > out[j].Score
	})
	if q.TopK > 0 && len(out) > q.TopK {
		out = out[:q.TopK]
	}
	return out
}

func lexicalTerms(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.')
	})
}

// LexicalTerms exposes the ranker's tokenization to storage candidate indexes.
// Candidate indexes must return a superset; RankTextCandidates remains the
// authority for matching and ordering.
func LexicalTerms(s string) []string { return lexicalTerms(s) }

// QueryIdentifiers returns mixed letter/digit tokens that callers generally
// expect to match literally (invoice numbers, VIN fragments, ticket IDs).
func QueryIdentifiers(s string) []string {
	var ids []string
	for _, term := range lexicalTerms(s) {
		letter, digit := false, false
		for _, r := range term {
			letter = letter || unicode.IsLetter(r)
			digit = digit || unicode.IsDigit(r)
		}
		if letter && digit {
			ids = append(ids, term)
		}
	}
	return ids
}

func MatchesIdentifiers(text string, identifiers []string) bool {
	if len(identifiers) == 0 {
		return true
	}
	tokens := make(map[string]bool)
	for _, term := range lexicalTerms(text) {
		tokens[term] = true
	}
	for _, id := range identifiers {
		if !tokens[id] {
			return false
		}
	}
	return true
}

func QuotedPhrases(text string) []string {
	var phrases []string
	for {
		start := strings.IndexByte(text, '"')
		if start < 0 {
			break
		}
		text = text[start+1:]
		end := strings.IndexByte(text, '"')
		if end < 0 {
			break
		}
		if phrase := strings.ToLower(strings.TrimSpace(text[:end])); phrase != "" {
			phrases = append(phrases, phrase)
		}
		text = text[end+1:]
	}
	return phrases
}
