package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Local is an embedder that calls a local sidecar HTTP service.
// Compatible with any service that exposes a POST /embed endpoint
// accepting {"texts": [...]} and returning {"embeddings": [[...], ...]}.
type Local struct {
	baseURL    string
	dimensions int
	httpClient *http.Client
}

// NewLocal returns a local sidecar embedder.
// baseURL is the sidecar address (e.g. "http://localhost:8080").
func NewLocal(baseURL string, dimensions int) *Local {
	return &Local{
		baseURL:    baseURL,
		dimensions: dimensions,
		httpClient: &http.Client{},
	}
}

func (e *Local) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := map[string]any{"texts": texts}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return embedResp.Embeddings, nil
}

func (e *Local) Dimensions() int {
	return e.dimensions
}
