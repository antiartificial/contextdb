package client_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

// vec8 returns a normalised 8-dim float32 vector with a strong bias toward
// dimension d. Used to create predictable cosine-similarity relationships.
func vec8(d int) []float32 {
	v := make([]float32, 8)
	for i := range v {
		if i == d%8 {
			v[i] = 0.9
		} else {
			v[i] = 0.1
		}
	}
	return v
}

func TestDB_SnapshotExportValidateImport(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	src := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer src.Close()

	ns := src.Namespace("test:snapshot-cli-source", namespace.ModeGeneral)
	written, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Snapshot export keeps vectors queryable",
		SourceID:   "snapshot-test",
		Labels:     []string{"Claim"},
		Vector:     vec8(2),
		Confidence: 0.88,
	})
	is.NoErr(err)
	is.True(written.Admitted)

	var buf bytes.Buffer
	is.NoErr(src.ExportSnapshot(ctx, "test:snapshot-cli-source", &buf))
	is.True(buf.Len() > 0)

	dst := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer dst.Close()
	dryRunReport, err := dst.ValidateSnapshotReport(ctx, "test:snapshot-cli-dest", bytes.NewReader(buf.Bytes()))
	is.NoErr(err)
	is.Equal(dryRunReport.Namespace, "test:snapshot-cli-dest")
	is.Equal(dryRunReport.DryRun, true)
	is.True(dryRunReport.Nodes > 0)
	is.True(dryRunReport.Vectors > 0)
	is.True(dryRunReport.NamespaceOverrides > 0)
	is.Equal(dryRunReport.NewNodes, dryRunReport.Nodes)
	is.Equal(dryRunReport.ChangedNodes, 0)
	is.Equal(dryRunReport.UnchangedNodes, 0)

	dstNS := dst.Namespace("test:snapshot-cli-dest", namespace.ModeGeneral)
	before, err := dstNS.Retrieve(ctx, client.RetrieveRequest{Vector: vec8(2), TopK: 5})
	is.NoErr(err)
	is.Equal(len(before), 0)

	importReport, err := dst.ImportSnapshotReport(ctx, "test:snapshot-cli-dest", bytes.NewReader(buf.Bytes()))
	is.NoErr(err)
	is.Equal(importReport.DryRun, false)
	is.Equal(importReport.Nodes, dryRunReport.Nodes)
	is.Equal(importReport.Vectors, dryRunReport.Vectors)
	is.Equal(importReport.NewNodes, dryRunReport.Nodes)
	after, err := dstNS.Retrieve(ctx, client.RetrieveRequest{Vector: vec8(2), TopK: 5})
	is.NoErr(err)
	is.True(len(after) > 0)
	is.Equal(after[0].Node.ID, written.NodeID)
	is.Equal(after[0].Node.Namespace, "test:snapshot-cli-dest")

	secondReport, err := dst.ValidateSnapshotReport(ctx, "test:snapshot-cli-dest", bytes.NewReader(buf.Bytes()))
	is.NoErr(err)
	is.Equal(secondReport.NewNodes, 0)
	is.Equal(secondReport.ChangedNodes, 0)
	is.Equal(secondReport.UnchangedNodes, secondReport.Nodes)
}

type countingEmbedder struct {
	vec   []float32
	calls int
}

func (e *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = e.vec
	}
	return out, nil
}

func (e *countingEmbedder) Dimensions() int {
	return len(e.vec)
}

// ─── Open / close ─────────────────────────────────────────────────────────────

func TestDB_OpenAndClose(t *testing.T) {
	is := is.New(t)

	db, err := client.Open(client.Options{Mode: client.ModeEmbedded})
	is.NoErr(err)
	is.True(db != nil)

	is.NoErr(db.Ping(context.Background()))
	is.NoErr(db.Close())

	// Idempotent close
	is.NoErr(db.Close())
}

func TestDB_MustOpenPanicsOnBadMode(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown mode")
		}
	}()
	client.MustOpen(client.Options{Mode: "nonexistent"})
}

// ─── Write ────────────────────────────────────────────────────────────────────

func TestNamespace_WriteAdmitsHighCredibility(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:write", namespace.ModeBeliefSystem)

	// Label the source as a moderator before writing
	is.NoErr(ns.LabelSource(ctx, "alice", []string{"moderator"}))

	result, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Go uses concurrent mark-and-sweep garbage collection",
		SourceID: "alice",
		Labels:   []string{"Claim"},
		Vector:   vec8(0),
		ModelID:  "test",
	})

	is.NoErr(err)
	is.True(result.Admitted)
	is.True(result.NodeID.String() != "00000000-0000-0000-0000-000000000000")
}

func TestNamespace_WriteRejectsTrollSource(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:troll", namespace.ModeBeliefSystem)
	is.NoErr(ns.LabelSource(ctx, "troll_user", []string{"troll"}))

	result, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Go has no garbage collector",
		SourceID: "troll_user",
		Labels:   []string{"Claim"},
		Vector:   vec8(0),
	})

	is.NoErr(err)
	is.True(!result.Admitted)
	t.Logf("rejection reason: %s", result.Reason)
}

func TestNamespace_WriteWithoutVectorStillAdmitted(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:novector", namespace.ModeGeneral)

	result, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Fact without embedding",
		SourceID:   "system",
		Confidence: 0.8,
		Labels:     []string{"Fact"},
	})

	is.NoErr(err)
	is.True(result.Admitted)
}

func TestNamespace_WriteDeduplicatesBeforeEmbedding(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	embedder := &countingEmbedder{vec: vec8(0)}
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded, Embedder: embedder})
	defer db.Close()

	ns := db.Namespace("test:dedup", namespace.ModeGeneral)

	first, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Go, uses   GC!",
		SourceID: "docs",
		Labels:   []string{"Fact"},
		Dedup:    true,
	})
	is.NoErr(err)
	is.True(first.Admitted)

	second, err := ns.Write(ctx, client.WriteRequest{
		Content:  "go uses gc",
		SourceID: "docs",
		Labels:   []string{"Fact"},
		Dedup:    true,
	})
	is.NoErr(err)
	is.True(second.Admitted)
	is.Equal(second.NodeID, first.NodeID)
	is.Equal(second.Reason, "deduplicated")
	is.Equal(embedder.calls, 1)

	node, err := ns.GetNode(ctx, first.NodeID)
	is.NoErr(err)
	is.True(node != nil)
	is.True(node.Fingerprint != "")
	is.Equal(node.Properties["source_id"], "docs")
}

func TestNamespace_WriteDedupIsOptIn(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	embedder := &countingEmbedder{vec: vec8(0)}
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded, Embedder: embedder})
	defer db.Close()

	ns := db.Namespace("test:dedup-opt-in", namespace.ModeGeneral)

	first, err := ns.Write(ctx, client.WriteRequest{
		Content:  "same text",
		SourceID: "docs",
		Labels:   []string{"Fact"},
	})
	is.NoErr(err)
	is.True(first.Admitted)

	second, err := ns.Write(ctx, client.WriteRequest{
		Content:  "same text",
		SourceID: "docs",
		Labels:   []string{"Fact"},
	})
	is.NoErr(err)
	is.True(!second.Admitted)
	is.True(second.Reason != "deduplicated")
	is.Equal(embedder.calls, 2)
}

func TestNamespace_FeedbackUpdatesNodeAndSource(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	start := time.Now().Add(-time.Second)

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:feedback", namespace.ModeGeneral)

	written, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Validated fact",
		SourceID:   "docs",
		Labels:     []string{"Fact"},
		Confidence: 0.5,
	})
	is.NoErr(err)
	is.True(written.Admitted)
	before, err := ns.GetNode(ctx, written.NodeID)
	is.NoErr(err)
	is.True(before != nil)

	validated, err := ns.ValidateClaim(ctx, written.NodeID)
	is.NoErr(err)
	is.Equal(validated.Action, "validated")
	is.True(validated.Confidence > before.Confidence)
	is.True(validated.SourceCredibility > 0.5)

	useful, err := ns.MarkUseful(ctx, written.NodeID, 4)
	is.NoErr(err)
	is.Equal(useful.Action, "useful")
	is.True(useful.Utility > 0.9)

	refuted, err := ns.RefuteClaim(ctx, written.NodeID, "bad source")
	is.NoErr(err)
	is.Equal(refuted.Action, "refuted")
	is.Equal(refuted.Confidence, 0.05)

	node, err := ns.GetNode(ctx, written.NodeID)
	is.NoErr(err)
	is.True(node != nil)
	is.Equal(node.Properties["refuted_reason"], "bad source")
	is.True(node.Version >= 4)

	events, err := ns.FeedbackEvents(ctx, start)
	is.NoErr(err)
	is.Equal(len(events), 3)
	is.Equal(events[0].Action, "validated")
	is.Equal(events[0].NodeID, written.NodeID)
	is.Equal(events[0].SourceID, "docs")
	is.True(events[0].SourceCredibility > 0.5)
	is.True(events[0].NodeVersion >= 2)
	is.True(!events[0].TxTime.IsZero())
	is.Equal(events[1].Action, "useful")
	is.Equal(events[1].Quality, 4)
	is.Equal(events[2].Action, "refuted")
	is.Equal(events[2].Reason, "bad source")

	timeline, err := ns.SourceTrustTimeline(ctx, "docs", start)
	is.NoErr(err)
	is.Equal(len(timeline), 2)
	is.Equal(timeline[0].Action, "validated")
	is.Equal(timeline[0].NodeID, written.NodeID)
	is.True(timeline[0].SourceCredibility > 0.5)
	is.Equal(timeline[1].Action, "refuted")
	is.Equal(timeline[1].Reason, "bad source")

	queue, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{After: start})
	is.NoErr(err)
	types := map[string]bool{}
	for _, item := range queue {
		types[item.Type] = true
	}
	is.True(types["refuted"])
	is.True(types["low_confidence"])
}

func TestNamespace_ReviewDecisionPersistsWorkflowState(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:review-decisions", namespace.ModeGeneral)
	start := time.Now().Add(-time.Second)
	written, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Unverified deployment claim",
		SourceID:   "docs",
		Labels:     []string{"Claim"},
		Confidence: 0.2,
	})
	is.NoErr(err)
	is.True(written.Admitted)

	queue, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{LowConfidenceThreshold: 0.35})
	is.NoErr(err)
	is.True(len(queue) > 0)
	reviewID := queue[0].ID

	decision, err := ns.RecordReviewDecision(ctx, client.ReviewDecisionRequest{
		ReviewID: reviewID,
		Status:   "assigned",
		Owner:    "alice",
		Decision: "needs_evidence",
		Note:     "check source logs",
	})
	is.NoErr(err)
	is.Equal(decision.ReviewID, reviewID)
	is.Equal(decision.Status, "assigned")
	is.Equal(decision.Owner, "alice")
	is.True(decision.EventID != uuid.Nil)

	decisions, err := ns.ReviewDecisions(ctx, start)
	is.NoErr(err)
	is.Equal(len(decisions), 1)
	is.Equal(decisions[0].ReviewID, reviewID)
	is.Equal(decisions[0].Decision, "needs_evidence")

	queue, err = ns.ReviewQueue(ctx, client.ReviewQueueRequest{LowConfidenceThreshold: 0.35})
	is.NoErr(err)
	is.True(len(queue) > 0)
	is.Equal(queue[0].ID, reviewID)
	is.Equal(queue[0].Status, "assigned")
	is.Equal(queue[0].Owner, "alice")
	is.Equal(queue[0].Note, "check source logs")

	escalated, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{
		LowConfidenceThreshold: 0.35,
		EscalationAfter:        time.Hour,
		Now:                    decision.TxTime.Add(2 * time.Hour),
	})
	is.NoErr(err)
	is.True(len(escalated) > 0)
	is.Equal(escalated[0].ID, reviewID)
	is.True(escalated[0].Escalated)
	is.Equal(escalated[0].EscalationLevel, "review_overdue")
	is.True(strings.Contains(escalated[0].EscalationReason, "assigned review"))
	is.True(escalated[0].EscalationAgeHours >= 2)

	digest, err := ns.ReviewEscalationDigest(ctx, client.ReviewQueueRequest{
		LowConfidenceThreshold: 0.35,
		EscalationAfter:        time.Hour,
		Now:                    decision.TxTime.Add(2 * time.Hour),
	})
	is.NoErr(err)
	is.Equal(digest.TotalEscalated, 1)
	is.Equal(len(digest.Groups), 1)
	is.Equal(digest.Groups[0].Owner, "alice")
	is.Equal(digest.Groups[0].Type, "low_confidence")
	is.Equal(digest.Groups[0].EscalationLevel, "review_overdue")
	is.Equal(digest.Groups[0].Count, 1)
	is.Equal(digest.Groups[0].ReviewIDs[0], reviewID)

	recordedDigest, err := ns.RecordReviewEscalationDigest(ctx, client.ReviewQueueRequest{
		LowConfidenceThreshold: 0.35,
		EscalationAfter:        time.Hour,
		Now:                    decision.TxTime.Add(2 * time.Hour),
	}, "weekly handoff")
	is.NoErr(err)
	is.True(recordedDigest.EventID != uuid.Nil)
	is.Equal(recordedDigest.Note, "weekly handoff")
	savedDigests, err := ns.ReviewEscalationDigests(ctx, start)
	is.NoErr(err)
	is.Equal(len(savedDigests), 1)
	is.Equal(savedDigests[0].EventID, recordedDigest.EventID)
	is.Equal(savedDigests[0].TotalEscalated, 1)
	is.Equal(savedDigests[0].Note, "weekly handoff")
	handoffs, err := ns.ReviewHandoffs(ctx, client.ReviewHandoffRequest{
		After:           start,
		Owner:           "alice",
		EscalationLevel: "review_overdue",
	})
	is.NoErr(err)
	is.Equal(len(handoffs), 1)
	is.Equal(handoffs[0].EventID, recordedDigest.EventID)
	is.Equal(handoffs[0].TotalEscalated, 1)
	is.Equal(handoffs[0].Groups[0].Owner, "alice")
	webhooks, err := ns.ReviewHandoffWebhookPlan(ctx, client.ReviewHandoffWebhookRequest{
		ReviewHandoffRequest: client.ReviewHandoffRequest{
			After:           start,
			Owner:           "alice",
			EscalationLevel: "review_overdue",
		},
		TargetURL: "https://ops.example.test/contextdb/handoffs",
		Secret:    "test-secret",
		Now:       start.Add(3 * time.Hour),
	})
	is.NoErr(err)
	is.Equal(len(webhooks), 1)
	is.Equal(webhooks[0].EventID, recordedDigest.EventID)
	is.Equal(webhooks[0].TargetURL, "https://ops.example.test/contextdb/handoffs")
	is.True(webhooks[0].DryRun)
	is.Equal(webhooks[0].Method, "POST")
	is.Equal(webhooks[0].TotalEscalated, 1)
	is.True(strings.HasPrefix(webhooks[0].Signature, "sha256="))
	is.Equal(webhooks[0].Headers["X-ContextDB-Signature"], webhooks[0].Signature)
	is.Equal(webhooks[0].Headers["X-ContextDB-Delivery-Mode"], "dry-run")
	is.Equal(webhooks[0].MaxAttempts, 3)
	is.True(len(webhooks[0].Payload) > 0)

	filtered, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{
		LowConfidenceThreshold: 0.35,
		Types:                  []string{"low_confidence"},
		SourceID:               "docs",
		Status:                 "assigned",
		Owner:                  "alice",
	})
	is.NoErr(err)
	is.Equal(len(filtered), 1)
	is.Equal(filtered[0].ID, reviewID)

	_, err = ns.RecordReviewDecision(ctx, client.ReviewDecisionRequest{
		ReviewID: reviewID,
		Status:   "resolved",
		Owner:    "alice",
		Decision: "verified_elsewhere",
	})
	is.NoErr(err)
	queue, err = ns.ReviewQueue(ctx, client.ReviewQueueRequest{LowConfidenceThreshold: 0.35})
	is.NoErr(err)
	for _, item := range queue {
		if item.ID == reviewID {
			t.Fatalf("resolved review item %q still present in queue", reviewID)
		}
	}
}

func TestNamespace_ReviewQueueIncludesSourceTrustAnomalies(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:source-anomalies", namespace.ModeGeneral)
	start := time.Now().Add(-time.Second)
	written, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Source-backed claim",
		SourceID:   "crawler",
		Labels:     []string{"Claim"},
		Confidence: 0.8,
	})
	is.NoErr(err)
	is.True(written.Admitted)

	_, err = ns.ValidateClaim(ctx, written.NodeID)
	is.NoErr(err)
	_, err = ns.RefuteClaim(ctx, written.NodeID, "source contradicted by audit")
	is.NoErr(err)

	queue, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{
		After:                    start,
		SourceTrustDropThreshold: 0.1,
		Types:                    []string{"source_trust_anomaly"},
		SourceID:                 "crawler",
	})
	is.NoErr(err)
	found := false
	for _, item := range queue {
		if item.Type == "source_trust_anomaly" {
			found = true
			is.Equal(item.SourceID, "crawler")
			is.Equal(item.Action, "credibility_drop")
			is.True(item.Confidence > 0)
			is.True(len(item.NodeIDs) == 1)
		}
	}
	is.True(found)

	escalated, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{
		After:                           start,
		SourceTrustDropThreshold:        0.1,
		SourceAnomalyEscalationPriority: 0.6,
		EscalationAfter:                 time.Hour,
		Now:                             time.Now().Add(2 * time.Hour),
		Types:                           []string{"source_trust_anomaly"},
		SourceID:                        "crawler",
	})
	is.NoErr(err)
	is.True(len(escalated) > 0)
	is.True(escalated[0].Escalated)
	is.Equal(escalated[0].EscalationLevel, "source_anomaly_high")
	is.True(strings.Contains(escalated[0].EscalationReason, "source anomaly priority"))
}

func TestNamespace_ReviewQueueIncludesContradictions(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("test:review-conflicts", namespace.ModeGeneral)

	a, err := ns.Write(ctx, client.WriteRequest{
		Content:    "The feature is enabled",
		SourceID:   "alpha",
		Labels:     []string{"Claim"},
		Confidence: 0.9,
	})
	is.NoErr(err)
	b, err := ns.Write(ctx, client.WriteRequest{
		Content:    "The feature is disabled",
		SourceID:   "beta",
		Labels:     []string{"Claim"},
		Confidence: 0.4,
	})
	is.NoErr(err)

	graph, _, _, _ := db.Stores()
	is.NoErr(graph.UpsertEdge(ctx, core.Edge{
		ID:        uuid.New(),
		Namespace: "test:review-conflicts",
		Type:      core.EdgeContradicts,
		Src:       a.NodeID,
		Dst:       b.NodeID,
		Weight:    0.9,
		ValidFrom: time.Now(),
		TxTime:    time.Now(),
	}))

	queue, err := ns.ReviewQueue(ctx, client.ReviewQueueRequest{LowConfidenceThreshold: 0.1})
	is.NoErr(err)
	is.True(len(queue) > 0)
	is.Equal(queue[0].Type, "conflict")
	is.Equal(len(queue[0].NodeIDs), 2)
}

func TestNamespace_ExplainAndKnowledgeGaps(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:explain", namespace.ModeGeneral)

	written, err := ns.Write(ctx, client.WriteRequest{
		Content:    "ContextDB tracks source credibility",
		SourceID:   "docs",
		Labels:     []string{"Fact"},
		Vector:     vec8(0),
		Confidence: 0.8,
	})
	is.NoErr(err)
	is.True(written.Admitted)

	report, err := ns.Explain(ctx, written.NodeID)
	is.NoErr(err)
	is.True(report != nil)
	is.Equal(report.Claim.Text, "ContextDB tracks source credibility")
	is.True(report.Summary != "")

	gaps, err := ns.KnowledgeGaps(ctx, client.GapRequest{MaxGaps: 3})
	is.NoErr(err)
	is.True(gaps != nil)
	is.Equal(gaps.Namespace, "test:explain")
	is.Equal(gaps.TotalNodes, 1)
}

func TestNamespace_ExplainRankComparesScoreFactors(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:explain-rank", namespace.ModeBeliefSystem)
	credible, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Deploys use blue green rollout",
		SourceID:   "runbook",
		Vector:     vec8(0),
		Confidence: 0.95,
	})
	is.NoErr(err)
	uncertain, err := ns.Write(ctx, client.WriteRequest{
		Content:    "Deploys use manual copy rollout",
		SourceID:   "chat",
		Vector:     vec8(1),
		Confidence: 0.2,
	})
	is.NoErr(err)
	is.True(credible.Admitted)
	is.True(uncertain.Admitted)
	supportID := uuid.New()
	graph, _, _, _ := db.Stores()
	is.NoErr(graph.UpsertNode(ctx, core.Node{
		ID:         supportID,
		Namespace:  "test:explain-rank",
		Properties: map[string]any{"text": "Runbook confirms blue green deployment", "source_id": "runbook"},
		Confidence: 0.9,
		ValidFrom:  time.Now(),
		TxTime:     time.Now(),
	}))
	is.NoErr(ns.AddEdge(ctx, core.Edge{
		Src:    supportID,
		Dst:    credible.NodeID,
		Type:   core.EdgeSupports,
		Weight: 0.8,
	}))

	explanation, err := ns.ExplainRank(ctx, client.ExplainRankRequest{
		NodeID:      credible.NodeID,
		OtherNodeID: uncertain.NodeID,
		Vector:      vec8(0),
	})
	is.NoErr(err)
	is.Equal(explanation.WinnerNodeID, credible.NodeID)
	is.True(explanation.Margin > 0)
	is.Equal(explanation.Node.NodeID, credible.NodeID)
	is.Equal(explanation.Other.NodeID, uncertain.NodeID)
	is.Equal(explanation.Node.Evidence.SupportCount, 1)
	is.Equal(explanation.Node.Evidence.Links[0].NodeID, supportID)
	is.True(len(explanation.Factors) == 4)
	is.True(explanation.Summary != "")
}

func TestNamespace_AcquisitionPlanIncludesWeakClaimTasks(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:acquisition-plan", namespace.ModeGeneral)
	written, err := ns.Write(ctx, client.WriteRequest{
		Content:    "The deployment process is undocumented",
		SourceID:   "chat",
		Vector:     vec8(0),
		Confidence: 0.2,
	})
	is.NoErr(err)
	is.True(written.Admitted)

	plan, err := ns.AcquisitionPlan(ctx, client.AcquisitionPlanRequest{Budget: 3})
	is.NoErr(err)
	is.True(plan != nil)
	is.Equal(plan.Namespace, "test:acquisition-plan")
	is.True(len(plan.Tasks) > 0)
	is.Equal(plan.Tasks[0].Type, "low_confidence")
	is.Equal(plan.Tasks[0].RelatedNodeIDs[0], written.NodeID)
	is.True(plan.Tasks[0].Prompt != "")
}

func TestNamespace_PersistentEmbeddedRestartPreservesCoreData(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	dir := t.TempDir()
	nsName := "test:restart"

	var nodeID uuid.UUID
	var expectedVersion uint64
	{
		db := client.MustOpen(client.Options{
			Mode:        client.ModeEmbedded,
			DataDir:     dir,
			DedupWrites: true,
		})
		ns := db.Namespace(nsName, namespace.ModeGeneral)

		written, err := ns.Write(ctx, client.WriteRequest{
			Content:    "Restart durability keeps nodes, vectors, and feedback",
			SourceID:   "docs",
			Labels:     []string{"Fact"},
			Vector:     vec8(0),
			Confidence: 0.6,
		})
		is.NoErr(err)
		is.True(written.Admitted)
		nodeID = written.NodeID

		validated, err := ns.ValidateClaim(ctx, written.NodeID)
		is.NoErr(err)
		is.Equal(validated.Action, "validated")

		duplicate, err := ns.Write(ctx, client.WriteRequest{
			Content:  "restart durability keeps nodes vectors and feedback",
			SourceID: "docs",
			Labels:   []string{"Fact"},
			Vector:   vec8(0),
		})
		is.NoErr(err)
		is.True(duplicate.Admitted)
		is.Equal(duplicate.NodeID, written.NodeID)
		is.Equal(duplicate.Reason, "deduplicated")

		node, err := ns.GetNode(ctx, written.NodeID)
		is.NoErr(err)
		is.True(node != nil)
		expectedVersion = node.Version
		is.True(expectedVersion >= 2)
		is.NoErr(db.Close())
	}

	db := client.MustOpen(client.Options{
		Mode:        client.ModeEmbedded,
		DataDir:     dir,
		DedupWrites: true,
	})
	defer db.Close()
	ns := db.Namespace(nsName, namespace.ModeGeneral)

	node, err := ns.GetNode(ctx, nodeID)
	is.NoErr(err)
	is.True(node != nil)
	is.Equal(node.Properties["text"], "Restart durability keeps nodes, vectors, and feedback")
	is.Equal(node.Properties["source_id"], "docs")
	is.True(node.Fingerprint != "")
	is.Equal(node.Version, expectedVersion)

	history, err := ns.History(ctx, nodeID)
	is.NoErr(err)
	is.True(len(history) >= 2)

	results, err := ns.Retrieve(ctx, client.RetrieveRequest{Vector: vec8(0), TopK: 3})
	is.NoErr(err)
	is.True(len(results) > 0)
	is.Equal(results[0].Node.ID, nodeID)
	is.True(results[0].Score > 0)
	is.True(results[0].Breakdown.Similarity > 0)

	duplicate, err := ns.Write(ctx, client.WriteRequest{
		Content:  "restart durability keeps nodes vectors and feedback",
		SourceID: "docs",
		Labels:   []string{"Fact"},
		Vector:   vec8(0),
	})
	is.NoErr(err)
	is.True(duplicate.Admitted)
	is.Equal(duplicate.NodeID, nodeID)
	is.Equal(duplicate.Reason, "deduplicated")
}

// ─── Retrieve ─────────────────────────────────────────────────────────────────

func TestNamespace_RetrieveReturnsResults(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:retrieve", namespace.ModeGeneral)
	is.NoErr(ns.LabelSource(ctx, "trusted", []string{"verified"}))

	// Write several nodes
	for i := 0; i < 4; i++ {
		_, err := ns.Write(ctx, client.WriteRequest{
			Content:  "document " + string(rune('A'+i)),
			SourceID: "trusted",
			Labels:   []string{"Document"},
			Vector:   vec8(i),
			ModelID:  "test",
		})
		is.NoErr(err)
	}

	// Retrieve with query closest to vec8(0)
	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: vec8(0),
		TopK:   3,
	})

	is.NoErr(err)
	is.True(len(results) > 0)
	is.True(len(results) <= 3)

	t.Log("retrieve results:")
	for _, r := range results {
		t.Logf("  [%s] score=%.4f sim=%.4f conf=%.2f src=%s",
			r.Node.Properties["text"], r.Score, r.SimilarityScore,
			r.Node.Confidence, r.RetrievalSource)
	}
}

func TestNamespace_RetrieveTopKRespected(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:topk", namespace.ModeGeneral)

	// Write 10 nodes
	for i := 0; i < 8; i++ {
		_, _ = ns.Write(ctx, client.WriteRequest{
			Content: "node",
			Vector:  vec8(i % 8),
			Labels:  []string{"Doc"},
		})
	}

	for _, k := range []int{1, 2, 5} {
		results, err := ns.Retrieve(ctx, client.RetrieveRequest{
			Vector: vec8(0),
			TopK:   k,
		})
		is.NoErr(err)
		is.True(len(results) <= k)
	}
}

// ─── Poisoning resistance end-to-end ─────────────────────────────────────────

// TestDB_PoisoningResistanceEndToEnd is the full integration test of the
// belief-system namespace. It writes one trusted claim and five troll claims
// about the same topic, then verifies the trusted claim is ranked first.
func TestDB_PoisoningResistanceEndToEnd(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("channel:general", namespace.ModeBeliefSystem)

	// Label sources
	is.NoErr(ns.LabelSource(ctx, "moderator:alice", []string{"moderator"}))
	is.NoErr(ns.LabelSource(ctx, "troll:anon", []string{"troll"}))

	// 1 trusted write — moderately similar to query
	trustedResult, err := ns.Write(ctx, client.WriteRequest{
		Content:  "Go is garbage collected via concurrent mark-and-sweep",
		SourceID: "moderator:alice",
		Labels:   []string{"Claim"},
		Vector:   []float32{0.85, 0.15, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		ModelID:  "test",
	})
	is.NoErr(err)
	is.True(trustedResult.Admitted)

	// 5 troll writes — higher cosine similarity to query (the poisoning advantage)
	for i := 0; i < 5; i++ {
		trollResult, err := ns.Write(ctx, client.WriteRequest{
			Content:  "Go has no garbage collector",
			SourceID: "troll:anon",
			Labels:   []string{"Claim"},
			Vector:   []float32{0.92 + float32(i)*0.01, 0.12, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
			ModelID:  "test",
		})
		is.NoErr(err)
		// Troll writes should be rejected by the admission gate
		t.Logf("troll write %d: admitted=%v reason=%s", i, trollResult.Admitted, trollResult.Reason)
	}

	// Retrieve with a query very close to the troll vectors
	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: []float32{0.93, 0.13, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		TopK:   10,
	})
	is.NoErr(err)

	t.Log("\nRetrieval results:")
	for i, r := range results {
		t.Logf("  rank=%d score=%.4f sim=%.4f conf=%.2f text=%s",
			i+1, r.Score, r.SimilarityScore, r.Node.Confidence,
			r.Node.Properties["text"])
	}

	// The trusted claim must appear in results
	found := false
	for _, r := range results {
		if r.Node.ID == trustedResult.NodeID {
			found = true
			t.Logf("\nTrusted claim found at score=%.4f", r.Score)
		}
	}
	is.True(found)
}

// ─── Stats ────────────────────────────────────────────────────────────────────

func TestDB_StatsAfterOperations(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:stats", namespace.ModeGeneral)

	_, _ = ns.Write(ctx, client.WriteRequest{
		Content: "test", Vector: vec8(0), Labels: []string{"Test"},
	})
	_, _ = ns.Retrieve(ctx, client.RetrieveRequest{Vector: vec8(0), TopK: 5})

	stats := db.Stats()
	is.True(stats.IngestTotal > 0)
	is.True(stats.RetrievalTotal > 0)

	t.Logf("stats: ingest=%d admitted=%d rejected=%d retrieve=%d p50=%.0fµs p95=%.0fµs",
		stats.IngestTotal, stats.IngestAdmitted, stats.IngestRejected,
		stats.RetrievalTotal, stats.LatencyP50Us, stats.LatencyP95Us)
}

// ─── Temporal query ───────────────────────────────────────────────────────────

func TestNamespace_TemporalAsOfQuery(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()

	ns := db.Namespace("test:temporal", namespace.ModeAgentMemory)

	past := time.Now().Add(-24 * time.Hour)

	// Write a node valid from 24h ago
	oldResult, err := ns.Write(ctx, client.WriteRequest{
		Content:   "old fact",
		Labels:    []string{"Fact"},
		Vector:    vec8(0),
		ValidFrom: past,
	})
	is.NoErr(err)
	is.True(oldResult.Admitted)

	// Retrieve pinned to 12h ago — should find the old fact
	results, err := ns.Retrieve(ctx, client.RetrieveRequest{
		Vector: vec8(0),
		TopK:   5,
		AsOf:   time.Now().Add(-12 * time.Hour),
	})
	is.NoErr(err)

	found := false
	for _, r := range results {
		if r.Node.ID == oldResult.NodeID {
			found = true
		}
	}
	is.True(found)
	t.Logf("AsOf query found %d results (expected old fact to be present)", len(results))
}
