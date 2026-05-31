package acquisition_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/acquisition"
)

func TestExecuteOpenAIConnectorNormalizesResponses(t *testing.T) {
	is := is.New(t)
	var request map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Header.Get("Authorization"), "Bearer test-key")
		is.NoErr(json.NewDecoder(r.Body).Decode(&request))
		tools := request["tools"].([]any)
		tool := tools[0].(map[string]any)
		is.Equal(tool["type"], "web_search")
		filters := tool["filters"].(map[string]any)
		is.Equal(filters["allowed_domains"].([]any)[0], "docs.example.com")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "The runbook says deployments should use atomic Helm upgrades. See https://docs.example.com/runbook.",
			"output": []map[string]any{
				{
					"type": "message",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": "Use helm upgrade --atomic.",
							"annotations": []map[string]any{
								{"type": "url_citation", "url": "https://docs.example.com/runbook"},
							},
						},
					},
				},
			},
		})
	}))
	defer upstream.Close()

	resp, err := acquisition.ExecuteProvider(context.Background(), acquisition.ProviderConfig{
		Provider:       acquisition.ProviderOpenAI,
		APIKey:         "test-key",
		BaseURL:        upstream.URL,
		Model:          "test-model",
		AllowedDomains: []string{"https://docs.example.com"},
	}, acquisition.ConnectorRequest{
		TaskID:           "low_confidence:1",
		TaskType:         "low_confidence",
		Query:            "deployment rollout evidence",
		Prompt:           "find evidence",
		AllowedSourceIDs: []string{"openai:web"},
		MaxResults:       3,
	})
	is.NoErr(err)
	is.Equal(request["model"], "test-model")
	is.Equal(len(resp.Items), 1)
	is.True(strings.Contains(resp.Items[0].Content, "atomic Helm upgrades"))
	is.Equal(resp.Items[0].SourceID, "openai:web")
	is.Equal(resp.Items[0].Metadata["provider"], "openai")
}

func TestExecuteAnthropicConnectorUsesMessagesWebSearchTool(t *testing.T) {
	is := is.New(t)
	var request map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Header.Get("x-api-key"), "anthropic-key")
		is.Equal(r.Header.Get("anthropic-version"), "2023-06-01")
		is.NoErr(json.NewDecoder(r.Body).Decode(&request))
		tools := request["tools"].([]any)
		tool := tools[0].(map[string]any)
		is.Equal(tool["type"], "web_search_20250305")
		is.Equal(tool["name"], "web_search")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": "Current docs confirm the API behavior.",
					"citations": []map[string]any{
						{"type": "web_search_result_location", "url": "https://docs.example.com/api", "title": "API docs"},
					},
				},
			},
		})
	}))
	defer upstream.Close()

	resp, err := acquisition.ExecuteProvider(context.Background(), acquisition.ProviderConfig{
		Provider: acquisition.ProviderAnthropic,
		APIKey:   "anthropic-key",
		BaseURL:  upstream.URL,
		Model:    "claude-test",
		MaxUses:  2,
	}, acquisition.ConnectorRequest{
		TaskID:           "research_gap:1",
		TaskType:         "research_gap",
		Query:            "api docs",
		AllowedSourceIDs: []string{"claude:web"},
		MaxResults:       1,
	})
	is.NoErr(err)
	is.Equal(request["model"], "claude-test")
	is.Equal(len(resp.Items), 1)
	is.Equal(resp.Items[0].URL, "https://docs.example.com/api")
	is.Equal(resp.Items[0].SourceID, "claude:web")
	is.Equal(resp.Items[0].Metadata["provider"], "anthropic")
}

func TestConnectorServerRoutesProvider(t *testing.T) {
	is := is.New(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "Found source https://docs.example.com/source",
		})
	}))
	defer upstream.Close()

	server := httptest.NewServer(acquisition.Server{Providers: map[string]acquisition.ProviderConfig{
		"openai": {
			Provider: acquisition.ProviderOpenAI,
			APIKey:   "key",
			BaseURL:  upstream.URL,
		},
	}}.Handler())
	defer server.Close()

	body := strings.NewReader(`{"task_id":"t","task_type":"research_gap","query":"q","allowed_source_ids":["openai:web"],"max_results":1}`)
	resp, err := http.Post(server.URL+"/openai/search", "application/json", body)
	is.NoErr(err)
	defer resp.Body.Close()
	is.Equal(resp.StatusCode, http.StatusOK)
	var out acquisition.ConnectorResponse
	is.NoErr(json.NewDecoder(resp.Body).Decode(&out))
	is.Equal(out.Items[0].SourceID, "openai:web")
}
