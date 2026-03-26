---
title: Examples
nav_order: 3
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

// Moderator writes -- always admitted, full confidence
ns.Write(ctx, client.WriteRequest{
    Content:    "The server runs Go 1.22",
    SourceID:   "moderator:alice",
    Labels:     []string{"Claim"},
    Vector:     embed("The server runs Go 1.22"),
    Confidence: 0.95,
})

// Unknown user writes -- admitted at neutral credibility (0.5)
ns.Write(ctx, client.WriteRequest{
    Content:  "The server uses Python",
    SourceID: "user:bob",
    Labels:   []string{"Claim"},
    Vector:   embed("The server uses Python"),
})

// Flag a troll -- future writes from this source are rejected
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

Store episodic memories from an autonomous agent. Memories decay naturally -- recent episodes rank higher than old ones.

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

// Retrieve -- recency and utility are weighted heavily
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

// Query as of yesterday -- returns the older value
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
