package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/server"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestRESTServerAcquisitionReviewRoutesValidateAndListCandidates(t *testing.T) {
	is := is.New(t)
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	handler := server.NewRESTServer(db).Handler()

	list := httptest.NewRequest(http.MethodGet, "/v1/namespaces/acquisition-review/acquisition/review/candidates", nil)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, list)
	is.Equal(listRecorder.Code, http.StatusOK)

	badID := httptest.NewRequest(http.MethodPost, "/v1/namespaces/acquisition-review/acquisition/review/candidates/not-a-uuid/approve", nil)
	badIDRecorder := httptest.NewRecorder()
	handler.ServeHTTP(badIDRecorder, badID)
	is.Equal(badIDRecorder.Code, http.StatusBadRequest)
}

func TestRESTServerAcquisitionExecuteDefersReviewBeforeAdmission(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("acquisition-review", namespace.ModeGeneral)
	seed, err := ns.Write(ctx, client.WriteRequest{Content: "This claim needs external verification", SourceID: "chat", Vector: []float32{1, 0, 0}, Confidence: 0.2})
	is.NoErr(err)
	is.True(seed.Admitted)
	connector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"content": "Operator reviewed acquisition evidence", "source_id": "docs"}}})
	}))
	defer connector.Close()
	body, err := json.Marshal(map[string]any{
		"budget": 1, "execute": true, "review_before_admission": true,
		"connectors": []map[string]any{{"id": "docs", "type": "search", "endpoint": connector.URL}},
	})
	is.NoErr(err)
	req := httptest.NewRequest(http.MethodPost, "/v1/namespaces/acquisition-review/acquisition/execute", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.NewRESTServer(db).Handler().ServeHTTP(recorder, req)
	is.Equal(recorder.Code, http.StatusOK)
	var result struct {
		Summary struct {
			WrittenNodes     int `json:"written_nodes"`
			ReviewCandidates int `json:"review_candidates"`
		} `json:"summary"`
	}
	is.NoErr(json.Unmarshal(recorder.Body.Bytes(), &result))
	is.Equal(result.Summary.WrittenNodes, 0)
	is.Equal(result.Summary.ReviewCandidates, 1)
}
