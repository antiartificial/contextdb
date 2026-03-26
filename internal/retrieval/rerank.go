package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/extract"
)

// Reranker reorders candidates using a cross-encoder or similar model.
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []core.Node, topK int) ([]core.ScoredNode, error)
}

// LLMReranker uses an LLM to rerank candidates by relevance.
type LLMReranker struct {
	llm   extract.Provider
	model string
}

// NewLLMReranker creates a reranker backed by an LLM provider.
func NewLLMReranker(llm extract.Provider, model string) *LLMReranker {
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &LLMReranker{llm: llm, model: model}
}

func (r *LLMReranker) Rerank(ctx context.Context, query string, candidates []core.Node, topK int) ([]core.ScoredNode, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if topK <= 0 || topK > len(candidates) {
		topK = len(candidates)
	}

	// Build prompt with candidates
	var sb strings.Builder
	sb.WriteString("Query: ")
	sb.WriteString(query)
	sb.WriteString("\n\nRank the following documents by relevance to the query. ")
	sb.WriteString("Return only the document numbers in order of relevance, most relevant first. ")
	sb.WriteString("Format: comma-separated numbers (e.g., 3,1,5,2,4)\n\n")

	for i, c := range candidates {
		text := ""
		if t, ok := c.Properties["text"].(string); ok {
			text = t
		}
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("Document %d: %s\n", i+1, text))
	}

	resp, err := r.llm.Chat(ctx, extract.ChatRequest{
		Model: r.model,
		Messages: []extract.ChatMessage{
			{Role: "system", Content: "You are a relevance ranking system. Output only comma-separated document numbers."},
			{Role: "user", Content: sb.String()},
		},
		Temperature: 0.0,
		MaxTokens:   100,
	})
	if err != nil {
		// Fallback: return candidates in original order
		return fallbackRerank(candidates, topK), nil
	}

	ranking := parseRanking(resp.Content, len(candidates))
	results := make([]core.ScoredNode, 0, topK)
	for i, idx := range ranking {
		if i >= topK {
			break
		}
		score := 1.0 - float64(i)/float64(len(ranking))
		results = append(results, core.ScoredNode{
			Node:            candidates[idx],
			Score:           score,
			SimilarityScore: score,
			RetrievalSource: "reranked",
		})
	}
	return results, nil
}

func fallbackRerank(candidates []core.Node, topK int) []core.ScoredNode {
	results := make([]core.ScoredNode, 0, topK)
	for i, c := range candidates {
		if i >= topK {
			break
		}
		results = append(results, core.ScoredNode{
			Node:            c,
			Score:           1.0 - float64(i)/float64(len(candidates)),
			SimilarityScore: 1.0 - float64(i)/float64(len(candidates)),
			RetrievalSource: "reranked",
		})
	}
	return results
}

func parseRanking(content string, maxIdx int) []int {
	content = strings.TrimSpace(content)
	parts := strings.Split(content, ",")
	seen := make(map[int]bool)
	var ranking []int

	for _, p := range parts {
		p = strings.TrimSpace(p)
		var idx int
		if _, err := fmt.Sscanf(p, "%d", &idx); err == nil {
			idx-- // convert 1-based to 0-based
			if idx >= 0 && idx < maxIdx && !seen[idx] {
				seen[idx] = true
				ranking = append(ranking, idx)
			}
		}
	}

	// Add any missing indices at the end
	for i := 0; i < maxIdx; i++ {
		if !seen[i] {
			ranking = append(ranking, i)
		}
	}

	return ranking
}
