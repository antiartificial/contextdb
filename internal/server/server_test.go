package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/pkg/client"
)

func startGRPCTestServer(t *testing.T, db *client.DB) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := grpc.NewServer(server.FormatGRPCCodec())
	server.NewGRPCService(db).Register(srv)

	go func() {
		if err := srv.Serve(lis); err != nil {
			t.Logf("gRPC test server stopped: %v", err)
		}
	}()
	t.Cleanup(func() { srv.GracefulStop() })

	return lis.Addr().String()
}

func TestRESTServer_Ping(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/v1/ping", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var resp map[string]string
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	is.Equal(resp["status"], "ok")
}

func TestRESTServer_Introspection(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	for _, path := range []string{"/v1/version", "/v1/features", "/v1/migrations"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		is.Equal(w.Code, http.StatusOK)
		var resp map[string]any
		is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
		is.Equal(resp["version"], buildinfo.Version)
	}

	req := httptest.NewRequest("GET", "/v1/version", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var versionResp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &versionResp))
	is.Equal(versionResp["api_version"], "v1")
	is.Equal(versionResp["latest_migration"], float64(2))
	features := versionResp["features"].([]any)
	is.True(len(features) > 0)
	migrations := versionResp["migrations"].([]any)
	is.Equal(len(migrations), 2)
}

func TestRESTServer_WriteAndRetrieve(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	// Write
	writeBody, _ := json.Marshal(map[string]any{
		"mode":       "belief_system",
		"content":    "Go uses goroutines for concurrency",
		"source_id":  "moderator:alice",
		"labels":     []string{"Claim"},
		"vector":     []float32{0.9, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
		"confidence": 0.95,
	})
	req := httptest.NewRequest("POST", "/v1/namespaces/channel:general/write", bytes.NewReader(writeBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var writeResp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &writeResp))
	is.Equal(writeResp["admitted"], true)
	nodeID := writeResp["node_id"].(string)

	// Retrieve
	retrieveBody, _ := json.Marshal(map[string]any{
		"vector": []float32{0.9, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
		"top_k":  5,
	})
	req2 := httptest.NewRequest("POST", "/v1/namespaces/channel:general/retrieve", bytes.NewReader(retrieveBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	is.Equal(w2.Code, http.StatusOK)
	var retrieveResp map[string]any
	is.NoErr(json.Unmarshal(w2.Body.Bytes(), &retrieveResp))
	results := retrieveResp["results"].([]any)
	is.True(len(results) > 0)
	first := results[0].(map[string]any)
	breakdown := first["score_breakdown"].(map[string]any)
	is.True(breakdown["similarity"].(float64) > 0)

	feedbackBody, _ := json.Marshal(map[string]any{"reason": "verified externally"})
	req3 := httptest.NewRequest("POST", "/v1/namespaces/channel:general/nodes/"+nodeID+"/validate", bytes.NewReader(feedbackBody))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	is.Equal(w3.Code, http.StatusOK)
	var feedbackResp map[string]any
	is.NoErr(json.Unmarshal(w3.Body.Bytes(), &feedbackResp))
	is.Equal(feedbackResp["action"], "validated")
	is.True(feedbackResp["source_credibility"].(float64) > 0.5)

	req4 := httptest.NewRequest("GET", "/v1/namespaces/channel:general/nodes/"+nodeID+"/narrative", nil)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, req4)

	is.Equal(w4.Code, http.StatusOK)
	var narrativeResp map[string]any
	is.NoErr(json.Unmarshal(w4.Body.Bytes(), &narrativeResp))
	is.True(narrativeResp["summary"].(string) != "")

	gapBody, _ := json.Marshal(map[string]any{"max_gaps": 2})
	req5 := httptest.NewRequest("POST", "/v1/namespaces/channel:general/gaps", bytes.NewReader(gapBody))
	req5.Header.Set("Content-Type", "application/json")
	w5 := httptest.NewRecorder()
	handler.ServeHTTP(w5, req5)

	is.Equal(w5.Code, http.StatusOK)
	var gapResp map[string]any
	is.NoErr(json.Unmarshal(w5.Body.Bytes(), &gapResp))
	is.Equal(gapResp["total_nodes"], float64(1))
}

func TestRESTServer_InvalidFeedbackNodeIDReturnsBadRequest(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	feedbackBody, _ := json.Marshal(map[string]any{"reason": "bad id"})
	req := httptest.NewRequest("POST", "/v1/namespaces/channel:general/nodes/not-a-uuid/validate", bytes.NewReader(feedbackBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusBadRequest)
}

func TestGRPCService_WriteRetrieveFeedbackContract(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{})
	defer db.Close()

	addr := startGRPCTestServer(t, db)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(server.GRPCCodec{})),
	)
	is.NoErr(err)
	defer conn.Close()

	var writeResp server.GRPCWriteResponse
	err = conn.Invoke(ctx, "/contextdb.v1.ContextDB/Write", &server.GRPCWriteRequest{
		Namespace:     "grpc-contract",
		NamespaceMode: "general",
		Content:       "gRPC contract returns score breakdown",
		SourceID:      "docs",
		Labels:        []string{"Fact"},
		Vector:        []float32{0.9, 0.1, 0.0, 0.0},
		Confidence:    0.7,
	}, &writeResp)
	is.NoErr(err)
	is.True(writeResp.Admitted)
	is.True(writeResp.NodeID != "")

	var retrieveResp server.GRPCRetrieveResponse
	err = conn.Invoke(ctx, "/contextdb.v1.ContextDB/Retrieve", &server.GRPCRetrieveRequest{
		Namespace: "grpc-contract",
		Vector:    []float32{0.9, 0.1, 0.0, 0.0},
		TopK:      3,
	}, &retrieveResp)
	is.NoErr(err)
	is.True(len(retrieveResp.Results) > 0)
	is.Equal(retrieveResp.Results[0].ID, writeResp.NodeID)
	is.True(retrieveResp.Results[0].Score > 0)
	is.True(retrieveResp.Results[0].ScoreBreakdown.Similarity > 0)

	var feedbackResp server.GRPCFeedbackResponse
	err = conn.Invoke(ctx, "/contextdb.v1.ContextDB/ValidateClaim", &server.GRPCFeedbackRequest{
		Namespace:     "grpc-contract",
		NamespaceMode: "general",
		NodeID:        writeResp.NodeID,
		Reason:        "verified through gRPC contract test",
	}, &feedbackResp)
	is.NoErr(err)
	is.Equal(feedbackResp.Action, "validated")
	is.Equal(feedbackResp.NodeID, writeResp.NodeID)
	is.Equal(feedbackResp.SourceID, "docs")
	is.True(feedbackResp.SourceCredibility > 0.5)
}

func TestGraphQLServer_SearchResolvesNodesAndSources(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	writeBody, _ := json.Marshal(map[string]any{
		"content":   "Go uses goroutines for concurrency",
		"source_id": "docs",
		"labels":    []string{"Fact"},
	})
	req := httptest.NewRequest("POST", "/v1/namespaces/graphql-test/write", bytes.NewReader(writeBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
	var writeResp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &writeResp))
	nodeID := writeResp["node_id"].(string)

	queryBody, _ := json.Marshal(map[string]any{
		"query": `{
			search(namespace: "graphql-test", query: "goroutines", limit: 5) {
				totalCount
				nodes {
					id
					content
					score
					scoreBreakdown { similarity }
					sources { name effectiveCredibility }
				}
			}
		}`,
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(queryBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var resp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql errors: %v", errs)
	}
	data := resp["data"].(map[string]any)
	search := data["search"].(map[string]any)
	is.Equal(search["totalCount"], float64(1))
	nodes := search["nodes"].([]any)
	node := nodes[0].(map[string]any)
	is.Equal(node["content"], "Go uses goroutines for concurrency")
	sources := node["sources"].([]any)
	is.Equal(sources[0].(map[string]any)["name"], "docs")

	mutationBody, _ := json.Marshal(map[string]any{
		"query": `mutation($id: ID!) {
			validateClaim(namespace: "graphql-test", nodeId: $id) {
				nodeId
				action
				confidence
				sourceCredibility
			}
		}`,
		"variables": map[string]any{"id": nodeID},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(mutationBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql mutation errors: %v", errs)
	}
	mutationData := resp["data"].(map[string]any)
	feedback := mutationData["validateClaim"].(map[string]any)
	is.Equal(feedback["action"], "validated")
	is.True(feedback["sourceCredibility"].(float64) > 0.5)

	queryBody, _ = json.Marshal(map[string]any{
		"query": `query($id: ID!) {
			narrative(namespace: "graphql-test", nodeId: $id) {
				summary
				claim { text }
			}
			knowledgeGaps(namespace: "graphql-test", maxGaps: 2) {
				totalNodes
				gapsDetected
			}
		}`,
		"variables": map[string]any{"id": nodeID},
	})
	req = httptest.NewRequest("POST", "/graphql", bytes.NewReader(queryBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	resp = map[string]any{}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql narrative/gaps errors: %v", errs)
	}
	data = resp["data"].(map[string]any)
	narrative := data["narrative"].(map[string]any)
	is.Equal(narrative["claim"].(map[string]any)["text"], "Go uses goroutines for concurrency")
	gaps := data["knowledgeGaps"].(map[string]any)
	is.Equal(gaps["totalNodes"], float64(1))
}

func TestGraphQLServer_Introspection(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	queryBody, _ := json.Marshal(map[string]any{
		"query": `{
			version {
				version
				apiVersion
				latestMigration
				features { name since status }
				migrations { version name }
			}
			features { name }
			migrations { version }
		}`,
	})
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(queryBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var resp map[string]any
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &resp))
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		t.Fatalf("graphql introspection errors: %v", errs)
	}
	data := resp["data"].(map[string]any)
	version := data["version"].(map[string]any)
	is.Equal(version["version"], buildinfo.Version)
	is.Equal(version["apiVersion"], "v1")
	is.Equal(version["latestMigration"], float64(2))
	is.True(len(version["features"].([]any)) > 0)
	is.Equal(len(version["migrations"].([]any)), 2)
	is.True(len(data["features"].([]any)) > 0)
	is.Equal(len(data["migrations"].([]any)), 2)
}

func TestRESTServer_Stats(t *testing.T) {
	is := is.New(t)

	db := client.MustOpen(client.Options{})
	defer db.Close()

	srv := server.NewRESTServer(db)
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/v1/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
}

func TestTenantMiddleware(t *testing.T) {
	is := is.New(t)

	var gotTenant string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = server.TenantFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := server.TenantMiddleware(inner)

	// Via X-Tenant-ID header
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", "acme-corp")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	is.Equal(gotTenant, "acme-corp")

	// Via Bearer token
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer tenant123:secrettoken")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	is.Equal(gotTenant, "tenant123")
}
