package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/matryer/is"
)

func TestAcquisitionReviewConcurrentTerminalDecision(t *testing.T) {
	is := is.New(t)
	db := MustOpen(Options{Mode: ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("candidate-race", namespace.ModeGeneral)
	candidate, err := ns.recordAcquisitionReviewCandidate(context.Background(), AcquisitionTask{ID: "task"}, AcquisitionConnector{ID: "connector"}, AcquisitionConnectorRun{IdempotencyKey: "run"}, AcquisitionPreviewItem{}, "candidate", "source", nil, time.Now())
	is.NoErr(err)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := ns.ApproveAcquisitionReviewCandidate(context.Background(), candidate.CandidateID, AcquisitionReviewDecisionRequest{})
		results <- err
	}()
	go func() {
		defer wg.Done()
		_, err := ns.RejectAcquisitionReviewCandidate(context.Background(), candidate.CandidateID, AcquisitionReviewDecisionRequest{})
		results <- err
	}()
	wg.Wait()
	close(results)
	decisions, err := ns.acquisitionReviewDecisions(context.Background())
	is.NoErr(err)
	decision := decisions[candidate.CandidateID]
	is.True(decision.Status == "admitted" || decision.Status == "rejected" || decision.Status == "admission_rejected")
}
