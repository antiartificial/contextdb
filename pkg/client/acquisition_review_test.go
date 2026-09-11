package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestAcquisitionReviewDefersAdmissionAndApprovesIdempotently(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("test:acquisition-review", namespace.ModeGeneral)
	seed, err := ns.Write(ctx, client.WriteRequest{Content: "The release process needs external verification", SourceID: "chat", Vector: vec8(0), Confidence: 0.2})
	is.NoErr(err)
	is.True(seed.Admitted)

	connector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{
			"content": "Release verification requires a tested rollback plan.", "source_id": "docs/runbook", "confidence": 0.9,
		}}})
	}))
	defer connector.Close()

	plan, err := ns.AcquisitionExecutionExecute(ctx, client.AcquisitionExecutionRequest{
		AcquisitionPlanRequest: client.AcquisitionPlanRequest{Budget: 1}, Execute: true, ReviewBeforeAdmission: true,
		Connectors: []client.AcquisitionConnector{{ID: "docs", Type: "search", Endpoint: connector.URL}},
	})
	is.NoErr(err)
	is.Equal(plan.Summary.WrittenNodes, 0)
	is.Equal(plan.Summary.ReviewCandidates, 1)
	is.Equal(len(plan.Runs[0].ReviewCandidateIDs), 1)

	candidates, err := ns.AcquisitionReviewCandidates(ctx, time.Time{})
	is.NoErr(err)
	is.Equal(len(candidates), 1)
	queue, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{})
	is.NoErr(err)
	found := false
	for _, item := range queue {
		found = found || item.Type == "acquisition_candidate" && item.ID == "acquisition_candidate:"+candidates[0].CandidateID.String()
	}
	is.True(found)

	approved, err := ns.ApproveAcquisitionReviewCandidate(ctx, candidates[0].CandidateID, client.AcquisitionReviewDecisionRequest{Actor: "operator", Note: "checked against runbook"})
	is.NoErr(err)
	is.True(approved.Write.Admitted)
	is.Equal(approved.Write.NodeID, candidates[0].NodeID)
	retry, err := ns.ApproveAcquisitionReviewCandidate(ctx, candidates[0].CandidateID, client.AcquisitionReviewDecisionRequest{Actor: "operator"})
	is.NoErr(err)
	is.Equal(retry.Decision.Status, "admitted")
	is.Equal(retry.Write.NodeID, candidates[0].NodeID)
}

func TestAcquisitionReviewRejectIsTerminalAndCandidateHistoryIsStable(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("test:acquisition-review-reject", namespace.ModeGeneral)
	seed, err := ns.Write(ctx, client.WriteRequest{Content: "A weak claim needs research", SourceID: "chat", Vector: vec8(0), Confidence: 0.2})
	is.NoErr(err)
	is.True(seed.Admitted)
	connector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"content": "Unreviewed acquisition", "source_id": "web"}})
	}))
	defer connector.Close()
	_, err = ns.AcquisitionExecutionExecute(ctx, client.AcquisitionExecutionRequest{AcquisitionPlanRequest: client.AcquisitionPlanRequest{Budget: 1}, Execute: true, ReviewBeforeAdmission: true, Connectors: []client.AcquisitionConnector{{ID: "web", Type: "search", Endpoint: connector.URL}}})
	is.NoErr(err)
	candidates, err := ns.AcquisitionReviewCandidates(ctx, time.Time{})
	is.NoErr(err)
	is.Equal(len(candidates), 1)
	rejected, err := ns.RejectAcquisitionReviewCandidate(ctx, candidates[0].CandidateID, client.AcquisitionReviewDecisionRequest{Actor: "operator", Note: "insufficient evidence"})
	is.NoErr(err)
	is.Equal(rejected.Decision.Status, "rejected")
	_, err = ns.ApproveAcquisitionReviewCandidate(ctx, candidates[0].CandidateID, client.AcquisitionReviewDecisionRequest{})
	is.True(err != nil)
	queue, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{})
	is.NoErr(err)
	for _, item := range queue {
		is.True(item.Type != "acquisition_candidate")
	}
}
