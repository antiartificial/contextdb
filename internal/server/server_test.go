package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/pkg/client"
)

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
