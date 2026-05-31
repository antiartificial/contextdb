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
	is.True(strings.Contains(w.Body.String(), "Belief Debugger"))
	is.True(strings.Contains(w.Body.String(), "/admin/api/belief"))
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
