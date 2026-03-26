package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAI is an OpenAI-compatible HTTP embedder. Works with OpenAI,
// Azure OpenAI, Ollama, vLLM, and any other OpenAI-compatible API.
type OpenAI struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	httpClient *http.Client
}

// OpenAIOption configures an OpenAI embedder.
type OpenAIOption func(*OpenAI)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) OpenAIOption {
	return func(o *OpenAI) { o.httpClient = c }
}

// NewOpenAI returns an OpenAI-compatible embedder.
// model is the embedding model name (e.g. "text-embedding-3-small").
// dimensions is the output vector size (e.g. 1536).
func NewOpenAI(baseURL, apiKey, model string, dimensions int, opts ...OpenAIOption) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	e := &OpenAI{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		httpClient: &http.Client{},
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := map[string]any{
		"model": e.model,
		"input": texts,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed API error %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	vectors := make([][]float32, len(embedResp.Data))
	for i, d := range embedResp.Data {
		vectors[i] = d.Embedding
	}
	return vectors, nil
}

func (e *OpenAI) Dimensions() int {
	return e.dimensions
}
