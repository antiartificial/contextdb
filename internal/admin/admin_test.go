package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/observe"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestAdminBeliefDebuggerAPI(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	graph, _, _, _ := db.Stores()
	ns := "debugger-test"
	now := time.Now().UTC()
	source := core.DefaultSource(ns, "docs")
	is.NoErr(graph.UpsertSource(ctx, source))
	claimID := uuid.New()
	claim := core.Node{
		ID:        claimID,
		Namespace: ns,
		Labels:    []string{"Claim"},
		Properties: map[string]any{
			"text":      "contextdb has an admin debugger",
			"source_id": "docs",
		},
		Confidence: 0.84,
		ValidFrom:  now,
		TxTime:     now,
	}
	is.NoErr(graph.UpsertNode(ctx, claim))
	supportID := uuid.New()
	support := core.Node{
		ID:         supportID,
		Namespace:  ns,
		Labels:     []string{"Evidence"},
		Properties: map[string]any{"text": "the observe port serves /admin/"},
		Confidence: 0.9,
		ValidFrom:  now,
		TxTime:     now,
	}
	is.NoErr(graph.UpsertNode(ctx, support))
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: ns,
		Src:       supportID,
		Dst:       claimID,
		Type:      core.EdgeSupports,
		ValidFrom: now,
		TxTime:    now,
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/api/belief?ns="+ns+"&id="+claimID.String(), nil)
	w := httptest.NewRecorder()
	New(db).ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var audit observe.BeliefAudit
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &audit))
	is.Equal(audit.Node.ID, claimID)
	is.True(audit.Source != nil)
	is.Equal(audit.Source.ExternalID, "docs")
	is.Equal(len(audit.Supporters), 1)
	is.Equal(audit.Supporters[0].Node.ID, supportID)
}

func TestAdminDashboardIncludesDebugger(t *testing.T) {
	is := is.New(t)
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	New(db).ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	is.True(strings.Contains(w.Body.String(), "Metrics"))
	is.True(strings.Contains(w.Body.String(), "Belief Debugger"))
	is.True(strings.Contains(w.Body.String(), "/admin/api/metrics"))
	is.True(strings.Contains(w.Body.String(), "/admin/api/search"))
	is.True(strings.Contains(w.Body.String(), "/admin/api/belief"))
}

func TestAdminMetricsAPI(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("metrics-test", namespace.ModeGeneral)
	_, err := ns.Write(ctx, client.WriteRequest{
		Content:    "admin metrics should surface ingest and latency signals",
		SourceID:   "metrics-test",
		Labels:     []string{"Claim"},
		Vector:     []float32{1, 0, 0, 0, 0, 0, 0, 0},
		Confidence: 0.9,
	})
	is.NoErr(err)
	_, err = ns.Retrieve(ctx, client.RetrieveRequest{
		Text:   "metrics",
		Vector: []float32{1, 0, 0, 0, 0, 0, 0, 0},
		TopK:   1,
	})
	is.NoErr(err)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/metrics", nil)
	w := httptest.NewRecorder()
	New(db).ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var body adminMetricsSnapshot
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &body))
	is.Equal(body.Mode, "embedded")
	is.Equal(body.Health.Status, "healthy")
	is.True(body.Ingest.Total >= 1)
	is.True(body.Ingest.Admitted >= 1)
	is.True(body.Ingest.AdmissionRate > 0)
	is.True(body.Retrieval.Total >= 1)
	is.True(len(body.Health.Signals) > 0)
}

func TestAdminSearchAPI(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	graph, _, _, _ := db.Stores()
	ns := "search-test"
	now := time.Now().UTC()
	matchID := uuid.New()
	is.NoErr(graph.UpsertNode(ctx, core.Node{
		ID:        matchID,
		Namespace: ns,
		Labels:    []string{"Claim"},
		Properties: map[string]any{
			"text":      "ranking manifests need bundle verification",
			"source_id": "release-notes",
		},
		Confidence: 0.91,
		ValidFrom:  now,
		TxTime:     now,
	}))
	is.NoErr(graph.UpsertNode(ctx, core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "unrelated node"},
		Confidence: 0.4,
		ValidFrom:  now,
		TxTime:     now.Add(-time.Minute),
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/api/search?ns="+ns+"&q=bundle&limit=5", nil)
	w := httptest.NewRecorder()
	New(db).ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusOK)
	var body struct {
		Namespace string         `json:"namespace"`
		Count     int            `json:"count"`
		Results   []searchResult `json:"results"`
	}
	is.NoErr(json.Unmarshal(w.Body.Bytes(), &body))
	is.Equal(body.Namespace, ns)
	is.Equal(body.Count, 1)
	is.Equal(body.Results[0].ID, matchID)
	is.Equal(body.Results[0].MatchReason, "text")
}

func TestAdminBeliefDebuggerAPIRejectsBadRequest(t *testing.T) {
	is := is.New(t)
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	req := httptest.NewRequest(http.MethodGet, "/admin/api/belief?ns=debugger-test&id=not-a-uuid", nil)
	w := httptest.NewRecorder()
	New(db).ServeHTTP(w, req)

	is.Equal(w.Code, http.StatusBadRequest)
}
