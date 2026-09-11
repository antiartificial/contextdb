package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/antiartificial/contextdb/pkg/client"
	"github.com/google/uuid"
)

func TestReviewWorkUnitHTTPFlow(t *testing.T) {
	modes := []client.Options{{}}
	if dsn := os.Getenv("CONTEXTDB_TEST_POSTGRES_DSN"); dsn != "" {
		modes = append(modes, client.Options{Mode: client.ModeStandard, DSN: dsn})
	}
	for _, opts := range modes {
		t.Run(string(opts.Mode), func(t *testing.T) {
			db, err := client.Open(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			registry, _ := NewTokenRegistry(`["review:read,write:test-secret"]`)
			srv := httptest.NewServer(registry.Middleware(NewRESTServer(db).Handler()))
			defer srv.Close()
			ns := "work-unit-" + uuid.NewString()
			base := "/v1/namespaces/" + ns
			call := func(method, path string, body any) map[string]any {
				t.Helper()
				b, _ := json.Marshal(body)
				req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
				req.Header.Set("Authorization", "Bearer review:read,write:test-secret")
				req.Header.Set("Content-Type", "application/json")
				response, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, _ := io.ReadAll(response.Body)
				if response.StatusCode/100 != 2 {
					t.Fatalf("%s %s status %d: %s", method, path, response.StatusCode, data)
				}
				var result map[string]any
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			seed := call("POST", base+"/write", map[string]any{"content": "claim awaiting stronger evidence", "source_id": "seed", "confidence": .2})
			if seed["admitted"] != true {
				t.Fatal(seed)
			}
			connector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"items":[{"content":"New corroborating evidence","source_id":"reviewed-source","confidence":0.2}]}`)
			}))
			defer connector.Close()
			plan := call("POST", base+"/acquisition/execute", map[string]any{"execute": true, "review_before_admission": true, "budget": 1, "connectors": []any{map[string]any{"id": "test", "type": "search", "endpoint": connector.URL, "allowed_source_ids": []string{"reviewed-source"}}}})
			retried := call("POST", base+"/acquisition/execute", map[string]any{"execute": true, "review_before_admission": true, "budget": 1, "connectors": []any{map[string]any{"id": "test", "type": "search", "endpoint": connector.URL, "allowed_source_ids": []string{"reviewed-source"}}}})
			if retried["summary"].(map[string]any)["errors"] != float64(0) {
				t.Fatal(retried)
			}
			if plan["summary"].(map[string]any)["written_nodes"] != float64(0) {
				t.Fatal(plan)
			}
			candidates := call("GET", base+"/acquisition/review/candidates", nil)["candidates"].([]any)
			if len(candidates) == 0 {
				t.Fatal("no deferred evidence candidates")
			}
			candidate := candidates[0].(map[string]any)
			id := candidate["candidate_id"].(string)
			approved := call("POST", base+"/acquisition/review/candidates/"+id+"/approve", map[string]any{"actor": "test-reviewer", "note": "reviewed evidence"})
			if approved["decision"].(map[string]any)["status"] != "admitted" {
				t.Fatal(approved)
			}
			repeated := call("POST", base+"/acquisition/review/candidates/"+id+"/approve", map[string]any{})
			if repeated["decision"].(map[string]any)["node_id"] != approved["decision"].(map[string]any)["node_id"] {
				t.Fatal("approval duplicated node")
			}
			evaluator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"action":"validate","confidence":0.95,"reason":"reviewed supporting evidence"}`)
			}))
			defer evaluator.Close()
			t.Setenv("CONTEXTDB_REVIEW_WEBHOOK_URL", evaluator.URL)
			dry := call("POST", base+"/review/worker/cycle", map[string]any{"evaluator": "webhook", "allowed_actions": []string{"validate"}})
			if dry["dry_run"] != true || len(dry["planned"].([]any)) == 0 {
				t.Fatal(dry)
			}
			applied := call("POST", base+"/review/worker/cycle", map[string]any{"execute": true, "evaluator": "webhook", "allowed_actions": []string{"validate"}})
			if applied["status"] != "completed" || applied["resolved"].(float64) < 1 {
				t.Fatal(applied)
			}
			runs := call("GET", base+"/review/worker/runs", nil)["runs"].([]any)
			if len(runs) != 2 {
				t.Fatalf("expected two durable runs got %v", runs)
			}
			if pending := call("GET", base+"/recovery/pending", nil)["pending"].([]any); len(pending) != 0 {
				t.Fatal(pending)
			}
		})
	}
}
