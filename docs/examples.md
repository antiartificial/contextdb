---
title: Examples
---

# Examples

Batteries-included recipes for common use cases.

## Belief system: channel fact-tracker

Track facts from multiple sources in a chat channel. Low-credibility sources are automatically rejected. High-credibility sources carry more weight in retrieval.

```go
db := client.MustOpen(client.Options{})
defer db.Close()

ns := db.Namespace("channel:general", namespace.ModeBeliefSystem)

// Register a trusted moderator
ns.LabelSource(ctx, "moderator:alice", []string{"moderator"})

// Moderator writes: always admitted, full confidence
ns.Write(ctx, client.WriteRequest{
    Content:    "The server runs Go 1.22",
    SourceID:   "moderator:alice",
    Labels:     []string{"Claim"},
    Vector:     embed("The server runs Go 1.22"),
    Confidence: 0.95,
})

// Unknown user writes: admitted at neutral credibility (0.5)
ns.Write(ctx, client.WriteRequest{
    Content:  "The server uses Python",
    SourceID: "user:bob",
    Labels:   []string{"Claim"},
    Vector:   embed("The server uses Python"),
})

// Flag a troll: future writes from this source are rejected
ns.LabelSource(ctx, "user:spammer", []string{"troll"})

// This write is rejected at the admission gate
res, _ := ns.Write(ctx, client.WriteRequest{
    Content:  "Buy cheap tokens at scam.xyz",
    SourceID: "user:spammer",
    Vector:   embed("Buy cheap tokens at scam.xyz"),
})
// res.Admitted == false
// res.Reason == "source credibility below floor (< 0.05)"
```

## Agent memory: task-aware retrieval

Store episodic memories from an autonomous agent. Memories decay naturally, so recent episodes rank higher than old ones.

```go
ns := db.Namespace("agent:planner", namespace.ModeAgentMemory)

// Store an episodic memory with fast decay
ns.Write(ctx, client.WriteRequest{
    Content:  "User asked to refactor the auth module",
    SourceID: "agent:self",
    Labels:   []string{"Episode"},
    Vector:   embed("User asked to refactor the auth module"),
    MemType:  core.MemoryEpisodic, // half-life ~8.7 hours
})

// Store a semantic memory with slow decay
ns.Write(ctx, client.WriteRequest{
    Content:  "The auth module uses JWT tokens with RS256",
    SourceID: "agent:self",
    Labels:   []string{"Fact"},
    Vector:   embed("The auth module uses JWT tokens with RS256"),
    MemType:  core.MemorySemantic, // half-life ~1.4 days
})

// Retrieve: recency and utility are weighted heavily
results, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Vector: embed("What do I know about auth?"),
    TopK:   10,
})
```

## RAG pipeline: document retrieval

Use the general namespace mode for balanced similarity-first retrieval.

```go
ns := db.Namespace("docs", namespace.ModeGeneral)

// Ingest documents in batch
docs := []client.WriteRequest{
    {Content: "contextdb stores nodes in a temporal graph...", Vector: embed("..."), SourceID: "crawler"},
    {Content: "Each node carries a confidence score...", Vector: embed("..."), SourceID: "crawler"},
    {Content: "The scoring function combines four dimensions...", Vector: embed("..."), SourceID: "crawler"},
}
results, _ := ns.WriteBatch(ctx, docs)

// Retrieve with custom scoring (boost similarity, reduce recency)
results, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Vector: embed("How does scoring work?"),
    TopK:   5,
    ScoreParams: core.ScoreParams{
        SimilarityWeight: 0.60,
        ConfidenceWeight: 0.25,
        RecencyWeight:    0.10,
        UtilityWeight:    0.05,
    },
})
```

## Procedural memory: skill storage

Store learned procedures and workflows. Procedural memories decay very slowly (half-life ~29 days).

```go
ns := db.Namespace("skills", namespace.ModeProcedural)

// Store a learned skill
skillID, _ := ns.Write(ctx, client.WriteRequest{
    Content:    "To deploy: run make build, then docker push, then helm upgrade",
    SourceID:   "agent:learner",
    Labels:     []string{"Skill", "Deployment"},
    Vector:     embed("deploy build docker helm"),
    Confidence: 0.85,
    MemType:    core.MemoryProcedural,
})

// Link related skills with edges
ns.AddEdge(ctx, core.Edge{
    Src:    skillID.NodeID,
    Dst:    prerequisiteSkillID,
    Type:   "requires",
    Weight: 0.8,
})

// Walk the skill graph from a known skill
walkResults, _ := ns.Walk(ctx, []uuid.UUID{skillID.NodeID}, 3)
for _, wr := range walkResults {
    fmt.Printf("depth=%d  skill=%s\n", wr.Depth, wr.Node.Properties["text"])
}
```

## Temporal queries: point-in-time retrieval

Query what the database knew at a specific point in time.

```go
ns := db.Namespace("facts", namespace.ModeGeneral)

// Write a fact that's valid starting now
ns.Write(ctx, client.WriteRequest{
    Content:    "The API rate limit is 100 req/s",
    SourceID:   "config",
    Vector:     embed("API rate limit"),
    Confidence: 1.0,
})

// Later: write an updated fact
ns.Write(ctx, client.WriteRequest{
    Content:    "The API rate limit is 500 req/s",
    SourceID:   "config",
    Vector:     embed("API rate limit"),
    Confidence: 1.0,
})

// Query as of yesterday: returns the older value
yesterday := time.Now().Add(-24 * time.Hour)
results, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Vector: embed("What is the API rate limit?"),
    TopK:   1,
    AsOf:   yesterday,
})

// View the full version history of a node
history, _ := ns.History(ctx, nodeID)
for _, version := range history {
    fmt.Printf("v%d  tx=%s  text=%s\n",
        version.Version, version.TxTime, version.Properties["text"])
}
```

## REST API: curl examples

```bash
# Write a fact
curl -X POST http://localhost:7701/v1/namespaces/my-app/write \
  -H "Content-Type: application/json" \
  -d '{
    "content": "Go 1.22 added routing patterns",
    "source_id": "docs-crawler",
    "labels": ["Claim"],
    "vector": [0.1, 0.2, 0.3],
    "confidence": 0.9
  }'

# Retrieve
curl -X POST http://localhost:7701/v1/namespaces/my-app/retrieve \
  -H "Content-Type: application/json" \
  -d '{
    "vector": [0.1, 0.2, 0.3],
    "top_k": 5
  }'

# Get a specific node
curl http://localhost:7701/v1/namespaces/my-app/nodes/{node-id}

# Health check
curl http://localhost:7701/v1/ping

# Metrics
curl http://localhost:7702/metrics
```

## Query DSL: pipe syntax and CQL

```go
// Pipe syntax — natural for REPL and admin UI
q, _ := dsl.ParsePipe(`search "project deadlines" | where confidence > 0.7 | weight recency:high | top 10`)
req := dsl.ToRetrieveRequest(q)

// CQL — structured queries for apps and config
q, _ := dsl.ParseCQL(`
  FIND "Go routing patterns"
  IN NAMESPACE docs
  WHERE source.credibility >= 0.8
  FOLLOW contradicts DEPTH 2
  WEIGHT similarity=0.4, recency=0.4
  LIMIT 5
  RERANK
`)

// Counterfactual: what would results look like without bot sources?
q, _ := dsl.ParseCQL(`FIND "team updates" EXCLUDE SOURCES "bot-123", "spam-456"`)
```

## Belief reconciliation: when agents disagree

```go
// Two agents wrote conflicting claims about the same topic
// Agent A: "The deploy uses blue-green strategy" (confidence 0.9)
// Agent B: "The deploy uses canary releases" (confidence 0.7)

// Get the structured disagreement
diff, _ := retrieval.ComputeBeliefDiff(ctx, graph, "ops", nil)

for _, conflict := range diff.Conflicts {
    fmt.Printf("Conflict (gap: %.0f%%):\n", conflict.CredibilityGap*100)
    fmt.Printf("  A: %s (conf: %.1f, %d supporters)\n",
        conflict.ClaimA.Node.Properties["text"],
        conflict.ClaimA.Confidence,
        conflict.ClaimA.SupporterCount)
    fmt.Printf("  B: %s (conf: %.1f, %d supporters)\n",
        conflict.ClaimB.Node.Properties["text"],
        conflict.ClaimB.Confidence,
        conflict.ClaimB.SupporterCount)
}
// Output:
// Conflict (gap: 20%):
//   A: The deploy uses blue-green strategy (conf: 0.9, 3 supporters)
//   B: The deploy uses canary releases (conf: 0.7, 1 supporters)
```

## Narrative retrieval: "explain what you know"

```go
formatter := retrieval.NewNarrativeFormatter(graph, vecs)
report, _ := formatter.Explain(ctx, "ops", claimNodeID)

fmt.Println(report.Summary)
// "High confidence claim, from a highly credible source (92%),
//  supported by 3 piece(s) of evidence, with 1 active contradiction(s)."

fmt.Println(report.ConfidenceExplanation)
// "Confidence 90% based on: source credibility: 92%; 3 supporting claims;
//  1 contradicting claims (reducing confidence)."

for _, ev := range report.Evidence {
    fmt.Printf("  [%s] %s (conf: %.1f)\n", ev.SourceID, ev.Text, ev.Confidence)
}
// [deploy-logs] Blue-green confirmed in prod logs (conf: 0.95)
// [ops-runbook] Runbook documents blue-green procedure (conf: 0.88)
// [incident-report] Blue-green rollback saved us in outage (conf: 0.91)
```

## Knowledge gaps: "what don't I know?"

```go
detector := retrieval.NewGapDetector(graph, vecs)
gaps, _ := detector.DetectGaps(ctx, "docs", retrieval.GapQuery{TopK: 20, MaxGaps: 5})

report := retrieval.BuildGapReport("docs", gaps, totalNodes)
fmt.Printf("Coverage: %.0f%%\n", report.CoverageScore*100)
// Coverage: 73%

for _, gap := range gaps {
    fmt.Printf("Gap between: %v (density: %.2f)\n",
        gap.NearestTopics, gap.DensityScore)
}
// Gap between: [Go performance, memory allocation] (density: 0.18)
// Gap between: [authentication, session management] (density: 0.25)
// Gap between: [deployment, monitoring] (density: 0.31)
```

## Active learning: "what should I learn next?"

```go
learner := retrieval.NewActiveLearner(graph)
suggestions, _ := learner.Suggest(ctx, "docs", 5)

for _, s := range suggestions {
    fmt.Printf("[%s] priority=%.1f: %s\n", s.Type, s.Priority, s.Description)
}
// [low_confidence] priority=0.8: Low confidence claim needs supporting evidence: memory allocation patterns in Go...
// [refresh_stale] priority=0.7: Claim expiring soon, needs refresh: Go 1.21 release notes...
// [verify_claim] priority=0.7: Old high-confidence claim with active contradictions: GC pause time is under 1ms...
```

## GDPR erasure: audit-trailed right-to-be-forgotten

```go
processor := compact.NewGDPRProcessor(graph, vecs, kv, eventLog)
report, _ := processor.ProcessErasure(ctx, compact.ErasureRequest{
    Namespace: "chat",
    SourceID:  "user:alice",
    Reason:    "GDPR Article 17 request",
})

fmt.Printf("Erased: %d nodes, %d vectors, %d edges invalidated\n",
    report.NodesRetracted, report.VectorsDeleted, report.EdgesInvalidated)
// Erased: 47 nodes, 47 vectors, 23 edges invalidated
// Full audit trail preserved — retraction markers, not deletion
```

## Cascade retraction: when a source claim is wrong

```go
retractor := compact.NewBulkRetractor(graph)

// Retract everything from a discredited source
result, _ := retractor.RetractBySource(ctx, "facts", "fake-news-bot", "source discredited")
fmt.Printf("Retracted %d claims from fake-news-bot\n", result.NodesRetracted)

// Cascade: retract a claim and everything derived from it
result, _ = retractor.CascadeRetract(ctx, "facts", claimID, "base claim disproven", 5)
fmt.Printf("Cascade retracted %d nodes across %d levels\n",
    result.NodesRetracted, result.CascadeDepth)
// Cascade retracted 12 nodes across 3 levels
```

## Calibration: turning confidence into probability

```go
// Collect prediction outcomes over time
outcomes := []observe.PredictionOutcome{
    {Predicted: 0.9, Actual: 1.0},  // said 90% confident, was true
    {Predicted: 0.9, Actual: 1.0},
    {Predicted: 0.9, Actual: 0.0},  // said 90% confident, was false
    {Predicted: 0.3, Actual: 0.0},
    // ... hundreds more
}

brier := observe.BrierScore(outcomes)
ece := observe.ExpectedCalibrationError(outcomes, 10)
fmt.Printf("Brier: %.3f  ECE: %.3f\n", brier, ece)
// Brier: 0.142  ECE: 0.089

// Fit a Platt scaler to correct future predictions
scaler := &observe.PlattScaler{}
scaler.Fit(outcomes, 50)

// Now raw confidence 0.85 maps to calibrated probability
calibrated := scaler.Calibrate(0.85)
fmt.Printf("Raw: 0.85 → Calibrated: %.2f\n", calibrated)
// Raw: 0.85 → Calibrated: 0.79
```

## Federation: multi-instance shared memory

```bash
# Instance A
export CONTEXTDB_FEDERATION_ENABLED=true
export CONTEXTDB_FEDERATION_BIND_ADDR=:7710
export CONTEXTDB_FEDERATION_SEED_PEERS=instance-b:7710

# Instance B
export CONTEXTDB_FEDERATION_ENABLED=true
export CONTEXTDB_FEDERATION_BIND_ADDR=:7710
export CONTEXTDB_FEDERATION_SEED_PEERS=instance-a:7710

# Both instances now share namespaces via gossip protocol.
# Writes on instance A replicate to instance B within seconds.
# Source credibility merges additively in Beta space —
# two instances observing the same source produces more evidence, not conflicts.
```

## Interference detection: protecting established knowledge

```go
// High-credibility claim with strong evidence
ns.Write(ctx, client.WriteRequest{
    Content:    "The API uses OAuth 2.0 with PKCE",
    SourceID:   "security-audit",     // credibility: 0.95
    Confidence: 0.95,
    Labels:     []string{"Security"},
})
// (also has 3 supporting claims from other sources)

// Low-credibility source tries to contradict it
res, _ := ns.Write(ctx, client.WriteRequest{
    Content:    "The API uses basic auth",
    SourceID:   "random-user",        // credibility: 0.3
    Confidence: 0.3,
    Labels:     []string{"Security"},
})
// Contradiction edge is created (the disagreement is tracked),
// but the original claim's confidence is NOT reduced.
// Interference detection protects well-established claims from
// being eroded by low-credibility noise.
```

## Time-travel admin API

```bash
# What did the namespace look like on June 1st?
curl "http://localhost:7702/admin/api/timetravel?ns=facts&asof=2024-06-01"

# What changed between two dates?
curl "http://localhost:7702/admin/api/diff?ns=facts&from=2024-06-01&to=2024-06-15"
```
