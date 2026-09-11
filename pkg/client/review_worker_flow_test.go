package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/internal/store"
)

type workerFailResolutionLog struct {
	store.EventLog
	fail bool
}

func (l *workerFailResolutionLog) Append(ctx context.Context, e store.Event) error {
	if l.fail && e.Type == store.EventReviewDecision {
		var d ReviewDecision
		_ = json.Unmarshal(e.Payload, &d)
		if d.Status == "resolved" {
			l.fail = false
			return fmt.Errorf("injected resolution failure")
		}
	}
	return l.EventLog.Append(ctx, e)
}

func TestWorkerDryRunExecutionAuditAndAmbiguousFailure(t *testing.T) {
	ctx := context.Background()
	db, err := Open(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ns := db.Namespace("worker-flow", namespace.ModeGeneral)
	written, err := ns.Write(ctx, WriteRequest{Content: "A claim needing review", SourceID: "worker-test", Confidence: .2})
	if err != nil || !written.Admitted {
		t.Fatalf("write %+v %v", written, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"action":"validate","confidence":0.95,"reason":"independent evidence"}`))
	}))
	defer srv.Close()
	req := ReviewWorkerRequest{Evaluator: "webhook", WebhookURL: srv.URL, AllowedActions: []string{"validate"}}
	before, _ := ns.History(ctx, written.NodeID)
	run, err := ns.RunReviewWorker(ctx, req)
	if err != nil || len(run.Planned) != 1 || run.Resolved != 0 {
		t.Fatalf("dry run %+v %v", run, err)
	}
	after, _ := ns.History(ctx, written.NodeID)
	if len(after) != len(before) {
		t.Fatal("dry run mutated claim")
	}
	db.log = &workerFailResolutionLog{EventLog: db.log, fail: true}
	req.Execute = true
	run, err = ns.RunReviewWorker(ctx, req)
	if err != nil || run.Status != "needs_attention" || len(run.Errors) == 0 {
		t.Fatalf("failed run %+v %v", run, err)
	}
	mutated, _ := ns.History(ctx, written.NodeID)
	again, err := ns.RunReviewWorker(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	latest, _ := ns.History(ctx, written.NodeID)
	if len(latest) != len(mutated) || again.Resolved != 0 {
		t.Fatal("ambiguous mutation repeated")
	}
	items, err := ns.ReviewQueue(ctx, ReviewQueueRequest{Status: "assigned"})
	if err != nil || len(items) != 1 {
		t.Fatalf("manual queue %v %v", items, err)
	}
	runs, err := ns.ReviewWorkerRuns(ctx, time.Time{})
	if err != nil || len(runs) != 3 || runs[1].Status != "needs_attention" {
		t.Fatalf("durable summaries %+v %v", runs, err)
	}
}

func TestWorkerSkipsAcquisitionCandidate(t *testing.T) {
	ctx := context.Background()
	db, _ := Open(Options{})
	defer db.Close()
	ns := db.Namespace("skip-candidate", namespace.ModeGeneral)
	_, err := ns.recordAcquisitionReviewCandidate(ctx, AcquisitionTask{ID: "task"}, AcquisitionConnector{ID: "connector"}, AcquisitionConnectorRun{IdempotencyKey: "run"}, AcquisitionPreviewItem{}, "pending evidence", "source", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	run, err := ns.RunReviewWorker(ctx, ReviewWorkerRequest{Execute: true, Evaluator: "webhook", WebhookURL: srv.URL, AllowedActions: []string{"validate", "resolve"}})
	if err != nil || called || run.Resolved != 0 {
		t.Fatalf("candidate reached evaluator: %+v %v called=%t", run, err, called)
	}
}
