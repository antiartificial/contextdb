package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NLWriteRequest is a natural language instruction to store a memory.
type NLWriteRequest struct {
	// Text is the natural language input, e.g. "Remember that the standup is cancelled tomorrow"
	Text string
	// Namespace is the target namespace.
	Namespace string
	// SourceID identifies who is writing.
	SourceID string
}

// NLWriteResult is the structured claim extracted from natural language.
type NLWriteResult struct {
	// Content is the extracted factual claim.
	Content string `json:"content"`
	// Labels are topic/category labels for the claim.
	Labels []string `json:"labels,omitempty"`
	// Confidence is the LLM's assessment of how confident the speaker is (0-1).
	Confidence float64 `json:"confidence"`
	// EpistemicType classifies the claim: "assertion", "observation", or "inference".
	EpistemicType string `json:"epistemic_type"`
	// Properties are any additional structured data extracted.
	Properties map[string]any `json:"properties,omitempty"`
}

// NLWriter converts natural language instructions into structured claims
// suitable for writing to contextdb.
type NLWriter struct {
	llm Provider
}

// NewNLWriter creates a new natural language write interface.
func NewNLWriter(llm Provider) *NLWriter {
	return &NLWriter{llm: llm}
}

// Parse converts a natural language input into a structured write result.
func (w *NLWriter) Parse(ctx context.Context, req NLWriteRequest) (*NLWriteResult, error) {
	if w.llm == nil {
		return nil, fmt.Errorf("NLWriter requires an LLM provider")
	}

	prompt := fmt.Sprintf(`Extract a structured claim from this natural language input.

Input: %s

Respond with a JSON object containing:
- "content": the factual claim (rewritten as a clear, standalone statement)
- "labels": array of 1-3 topic labels
- "confidence": how confident the speaker seems (0.0-1.0, where hedging like "I think" = lower)
- "epistemic_type": one of "assertion" (stated as fact), "observation" (reported/uncertain), or "inference" (derived/concluded)
- "properties": any additional structured data (dates, names, etc.)

Output only valid JSON, nothing else.`, req.Text)

	resp, err := w.llm.Chat(ctx, ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a claim extraction system. Convert natural language into structured factual claims. Output only valid JSON."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.0,
		MaxTokens:   500,
	})
	if err != nil {
		return nil, fmt.Errorf("NLWriter: LLM call failed: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	// Strip markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result NLWriteResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("NLWriter: failed to parse LLM response: %w (raw: %s)", err, content)
	}

	// Apply defaults
	if result.Confidence == 0 {
		result.Confidence = 0.7
	}
	if result.EpistemicType == "" {
		result.EpistemicType = "assertion"
	}

	return &result, nil
}
