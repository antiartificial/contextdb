package acquisition

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	ProviderOpenAI    = "openai"
	ProviderXAI       = "xai"
	ProviderAnthropic = "anthropic"
)

// ConnectorRequest is the JSON contract sent by contextdb acquisition execution.
type ConnectorRequest struct {
	TaskID           string   `json:"task_id"`
	TaskType         string   `json:"task_type"`
	Query            string   `json:"query"`
	Prompt           string   `json:"prompt"`
	AllowedSourceIDs []string `json:"allowed_source_ids"`
	MaxResults       int      `json:"max_results"`
	ConnectorID      string   `json:"connector_id"`
	ConnectorType    string   `json:"connector_type"`
}

// ConnectorItem is the normalized result returned to contextdb.
type ConnectorItem struct {
	Title      string            `json:"title,omitempty"`
	URL        string            `json:"url,omitempty"`
	Snippet    string            `json:"snippet,omitempty"`
	Content    string            `json:"content,omitempty"`
	SourceID   string            `json:"source_id,omitempty"`
	Labels     []string          `json:"labels,omitempty"`
	Confidence float64           `json:"confidence,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ConnectorResponse is returned to the contextdb acquisition executor.
type ConnectorResponse struct {
	Items []ConnectorItem `json:"items"`
}

// ProviderConfig configures one provider-backed connector route.
type ProviderConfig struct {
	Provider       string
	APIKey         string
	Model          string
	BaseURL        string
	AllowedDomains []string
	BlockedDomains []string
	MaxUses        int
	HTTPClient     *http.Client
}

// Server serves provider-backed acquisition connector endpoints.
type Server struct {
	Providers map[string]ProviderConfig
}

// Handler returns HTTP routes for all configured providers.
func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	for name, cfg := range s.Providers {
		routeName := strings.Trim(strings.ToLower(name), "/")
		cfg.Provider = strings.TrimSpace(cfg.Provider)
		mux.HandleFunc("POST /"+routeName+"/search", providerHandler(cfg))
		mux.HandleFunc("POST /"+routeName+"/crawl", providerHandler(cfg))
	}
	return mux
}

func providerHandler(cfg ProviderConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ConnectorRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resp, err := ExecuteProvider(r.Context(), cfg, req)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// ExecuteProvider calls one provider and normalizes its response into connector items.
func ExecuteProvider(ctx context.Context, cfg ProviderConfig, req ConnectorRequest) (ConnectorResponse, error) {
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch cfg.Provider {
	case ProviderOpenAI:
		return executeOpenAICompatible(ctx, normalizeProviderConfig(cfg, "https://api.openai.com/v1", "gpt-5"), req, "openai")
	case ProviderXAI:
		return executeOpenAICompatible(ctx, normalizeProviderConfig(cfg, "https://api.x.ai/v1", "grok-4.3"), req, "xai")
	case ProviderAnthropic:
		return executeAnthropic(ctx, normalizeProviderConfig(cfg, "https://api.anthropic.com", "claude-sonnet-4-20250514"), req)
	default:
		return ConnectorResponse{}, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}

func executeOpenAICompatible(ctx context.Context, cfg ProviderConfig, req ConnectorRequest, provider string) (ConnectorResponse, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return ConnectorResponse{}, fmt.Errorf("%s connector: API key is required", provider)
	}
	if len(cfg.AllowedDomains) > 0 && len(cfg.BlockedDomains) > 0 {
		return ConnectorResponse{}, fmt.Errorf("%s connector: allowed and blocked domains are mutually exclusive", provider)
	}
	tool := map[string]any{"type": "web_search"}
	filters := map[string]any{}
	if len(cfg.AllowedDomains) > 0 {
		filters["allowed_domains"] = cfg.AllowedDomains
	}
	if len(cfg.BlockedDomains) > 0 {
		filters["excluded_domains"] = cfg.BlockedDomains
	}
	if len(filters) > 0 {
		tool["filters"] = filters
	}
	body := map[string]any{
		"model": cfg.Model,
		"input": []map[string]string{{
			"role":    "user",
			"content": providerPrompt(req),
		}},
		"tools": []map[string]any{tool},
	}
	raw, err := postJSON(ctx, cfg, strings.TrimRight(cfg.BaseURL, "/")+"/responses", map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}, body)
	if err != nil {
		return ConnectorResponse{}, err
	}
	return normalizeProviderResponse(provider, raw, req), nil
}

func executeAnthropic(ctx context.Context, cfg ProviderConfig, req ConnectorRequest) (ConnectorResponse, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return ConnectorResponse{}, fmt.Errorf("anthropic connector: API key is required")
	}
	if len(cfg.AllowedDomains) > 0 && len(cfg.BlockedDomains) > 0 {
		return ConnectorResponse{}, fmt.Errorf("anthropic connector: allowed and blocked domains are mutually exclusive")
	}
	tool := map[string]any{
		"type": "web_search_20250305",
		"name": "web_search",
	}
	if cfg.MaxUses > 0 {
		tool["max_uses"] = cfg.MaxUses
	}
	if len(cfg.AllowedDomains) > 0 {
		tool["allowed_domains"] = cfg.AllowedDomains
	}
	if len(cfg.BlockedDomains) > 0 {
		tool["blocked_domains"] = cfg.BlockedDomains
	}
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": 1024,
		"messages": []map[string]string{{
			"role":    "user",
			"content": providerPrompt(req),
		}},
		"tools": []map[string]any{tool},
	}
	raw, err := postJSON(ctx, cfg, strings.TrimRight(cfg.BaseURL, "/")+"/v1/messages", map[string]string{
		"x-api-key":         cfg.APIKey,
		"anthropic-version": "2023-06-01",
	}, body)
	if err != nil {
		return ConnectorResponse{}, err
	}
	return normalizeProviderResponse("anthropic", raw, req), nil
}

func postJSON(ctx context.Context, cfg ProviderConfig, endpoint string, headers map[string]string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func providerPrompt(req ConnectorRequest) string {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = strings.TrimSpace(req.Prompt)
	}
	return strings.TrimSpace(fmt.Sprintf(`Research this contextdb acquisition task and return concise source-grounded evidence.

Task type: %s
Query: %s
Prompt: %s

Prefer primary sources. Include URLs and short excerpts or summaries when available.`, req.TaskType, query, strings.TrimSpace(req.Prompt)))
}

func normalizeProviderResponse(provider string, raw []byte, req ConnectorRequest) ConnectorResponse {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ConnectorResponse{Items: []ConnectorItem{fallbackItem(provider, req, string(raw), "")}}
	}
	texts := collectStringsByKey(value, map[string]bool{
		"output_text": true,
		"text":        true,
		"content":     true,
		"summary":     true,
		"snippet":     true,
	})
	urls := collectURLs(value)
	urls = append(urls, collectURLsFromText(string(raw))...)
	urls = dedupe(urls)
	content := compactText(texts)
	if content == "" {
		content = strings.TrimSpace(req.Query)
	}
	urls = append(urls, collectURLsFromText(content)...)
	urls = dedupe(urls)
	sourceID := providerSourceID(provider, req)
	labels := []string{"acquired", "provider:" + provider}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	var items []ConnectorItem
	for _, foundURL := range urls {
		items = append(items, ConnectorItem{
			Title:      hostTitle(foundURL),
			URL:        foundURL,
			Snippet:    firstSentence(content),
			Content:    content,
			SourceID:   sourceID,
			Labels:     labels,
			Confidence: 0.72,
			Metadata: map[string]string{
				"provider": provider,
				"task_id":  req.TaskID,
			},
		})
		if len(items) >= maxResults {
			return ConnectorResponse{Items: items}
		}
	}
	return ConnectorResponse{Items: []ConnectorItem{fallbackItem(provider, req, content, "")}}
}

func fallbackItem(provider string, req ConnectorRequest, content, foundURL string) ConnectorItem {
	return ConnectorItem{
		Title:      strings.TrimSpace(req.Query),
		URL:        foundURL,
		Snippet:    firstSentence(content),
		Content:    content,
		SourceID:   providerSourceID(provider, req),
		Labels:     []string{"acquired", "provider:" + provider},
		Confidence: 0.62,
		Metadata: map[string]string{
			"provider": provider,
			"task_id":  req.TaskID,
		},
	}
}

func normalizeProviderConfig(cfg ProviderConfig, defaultBaseURL, defaultModel string) ProviderConfig {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	cfg.AllowedDomains = normalizeDomains(cfg.AllowedDomains)
	cfg.BlockedDomains = normalizeDomains(cfg.BlockedDomains)
	return cfg
}

func providerSourceID(provider string, req ConnectorRequest) string {
	for _, source := range req.AllowedSourceIDs {
		if strings.TrimSpace(source) != "" {
			return strings.TrimSpace(source)
		}
	}
	return provider + ":web"
}

func collectStringsByKey(value any, keys map[string]bool) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, child := range typed {
				if keys[strings.ToLower(key)] {
					switch s := child.(type) {
					case string:
						if strings.TrimSpace(s) != "" {
							out = append(out, strings.TrimSpace(s))
						}
					case []any, map[string]any:
						walk(child)
					}
				} else {
					walk(child)
				}
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return dedupe(out)
}

func collectURLs(value any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, child := range typed {
				if strings.Contains(strings.ToLower(key), "url") || strings.EqualFold(key, "source") {
					if s, ok := child.(string); ok && isHTTPURL(s) {
						out = append(out, s)
						continue
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			for _, token := range strings.Fields(typed) {
				token = strings.Trim(token, ".,;:()[]{}<>\"'")
				if isHTTPURL(token) {
					out = append(out, token)
				}
			}
		}
	}
	walk(value)
	return dedupe(out)
}

func collectURLsFromText(text string) []string {
	var out []string
	for _, scheme := range []string{"https://", "http://"} {
		remaining := text
		for {
			idx := strings.Index(remaining, scheme)
			if idx < 0 {
				break
			}
			candidate := remaining[idx:]
			end := len(candidate)
			for i, r := range candidate {
				if i == 0 {
					continue
				}
				if strings.ContainsRune(" \n\t\r\"'<>[](){}`", r) {
					end = i
					break
				}
			}
			token := strings.Trim(candidate[:end], ".,;:()\\")
			if isHTTPURL(token) {
				out = append(out, token)
			}
			remaining = candidate[end:]
		}
	}
	return dedupe(out)
}

func compactText(parts []string) string {
	var kept []string
	for _, part := range parts {
		part = strings.Join(strings.Fields(part), " ")
		if part == "" || isHTTPURL(part) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.TrimSpace(strings.Join(dedupe(kept), "\n\n"))
}

func firstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= 240 {
		return text
	}
	return text[:237] + "..."
}

func hostTitle(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	return parsed.Host
}

func isHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func normalizeDomains(domains []string) []string {
	var out []string
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if parsed, err := url.Parse(domain); err == nil && parsed.Host != "" {
			domain = parsed.Host
		}
		out = append(out, strings.Trim(domain, "/"))
	}
	sort.Strings(out)
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
