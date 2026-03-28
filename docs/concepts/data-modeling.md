---
title: Data Modeling
---

# Data Modeling

How you structure your data matters as much as what you store. This page covers the mechanics of how contextdb represents knowledge, the disambiguation problem that naive vector search can't solve, and practical strategies for modeling data that retrieves well.

## How contextdb stores knowledge

Every piece of knowledge is a **node**. A node has:

| Field | What it is |
|:------|:-----------|
| `Content` | The text claim being stored |
| `Vector` | An embedding of the content (auto-generated if omitted) |
| `Labels` | A set of string tags for hard filtering |
| `Properties` | A map of arbitrary key-value metadata |
| `SourceID` | Which source wrote this node |
| `Confidence` | A prior on how much to trust this claim (0.0-1.0) |
| `ValidFrom` / `ValidUntil` | When the claim is true in the world (bi-temporal) |

Nodes connect to other nodes via **typed edges**. An edge has a type (`supports`, `contradicts`, `derives_from`, `related_to`, etc.) and an optional weight.

Retrieval works across all of these layers simultaneously. Vector similarity finds candidates. Labels, source credibility, confidence, and recency rank them. Graph edges extend the reach of a query into related clusters. Skipping any of these layers means leaving retrieval quality on the table.

## The disambiguation problem

Take the word "strawberry." In an engineering org, it might be the name of an ML inference service. In a recipe blog, it's a fruit. Both will have similar embeddings for the bare word -- they're not synonyms, but they're the same token.

If you write both contexts into the same namespace and query with `"strawberry"`, a pure vector search returns both. Worse, the ranking is arbitrary because the embedding distance is nearly identical.

This is the disambiguation problem. Here's how contextdb's layers solve it.

### Labels as hard filters

Labels let you partition retrieval before scoring runs. The query only sees nodes that match all the requested labels.

```go
// Engineering context
ns.Write(ctx, client.WriteRequest{
    Content:  "Strawberry is our ML inference service, deployed on k8s",
    SourceID: "eng-wiki",
    Labels:   []string{"project", "infrastructure"},
})

// Food context
ns.Write(ctx, client.WriteRequest{
    Content:  "Fresh strawberries work best for shortcake, not frozen",
    SourceID: "recipe-blog",
    Labels:   []string{"recipe", "food"},
})

// Retrieve only engineering nodes -- the food node never enters the candidate set
results, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Text:   "strawberry",
    Labels: []string{"project"},
    TopK:   3,
})
```

The label filter is applied before scoring. It's not a soft signal -- nodes that don't match are excluded entirely.

### Query context carries intent

"strawberry" is ambiguous. "How do I deploy strawberry to production?" is not.

The full query embedding carries the deployment and infrastructure context, so it naturally scores higher against the engineering node even without label filters. In practice you want both: labels for hard isolation, rich query text for better ranking within the filtered set.

### Graph edges pull clusters together

Engineering knowledge about Strawberry doesn't live in a single node. There's the service description, the deployment runbook, the incident history, the architecture decision records. These nodes are connected via `supports` and `derives_from` edges.

When you retrieve using seed IDs alongside a query, the graph walk follows those edges and pulls in connected nodes -- even ones that don't rank highly on vector similarity alone. This means a query that finds the service description node also surfaces the runbook and recent incidents, because they're in the same cluster.

```go
// First retrieval finds the service description node
initial, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Text:   "How do I deploy Strawberry?",
    Labels: []string{"infrastructure"},
    TopK:   1,
})

// Follow the graph from that seed to pull in the runbook and related nodes
expanded, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Text:    "deployment steps",
    SeedIDs: []string{initial[0].ID},
    TopK:    5,
})
```

### Source credibility scopes by domain

`eng-wiki` has accumulated credibility for infrastructure topics. `recipe-blog` has credibility for food. When you configure domain-scoped credibility, a source's track record only applies to the topics it knows about.

A high-credibility food source doesn't get a boost for engineering queries. This is especially useful when you're mixing data origins -- internal wikis, external feeds, user input -- that are authoritative in some domains but not others.

### Weight tuning breaks ties

When vector similarity is nearly equal across candidates, the other scoring dimensions decide. You can push these at query time using `ScoreParams`:

```go
// Prioritize credibility over recency for infrastructure queries
results, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Text:   "Strawberry deployment process",
    Labels: []string{"infrastructure"},
    TopK:   5,
    ScoreParams: core.ScoreParams{
        SimilarityWeight: 0.35,
        ConfidenceWeight: 0.40,
        RecencyWeight:    0.15,
        UtilityWeight:    0.10,
    },
})
```

For a runbook query you care more about trusted sources than the freshest write. For an incident status query you might flip those weights.

## Strategies for effective data modeling

### Strategy 1: Label taxonomy

Design your labels like a shallow hierarchy. Don't go deep -- that's what edges and properties are for.

```go
// Good: broad categories that work as filters
Labels: []string{"incident", "postmortem", "backend"}

// Properties for the specific stuff
Properties: map[string]any{
    "severity": "p1",
    "service":  "auth",
    "team":     "platform",
}
```

Good labels are broad enough to be reusable across nodes: `["project", "backend"]`, `["incident", "p1"]`, `["user-feedback", "mobile"]`. Bad labels encode so much specificity that they can't be shared: `["project:backend:auth:jwt:token-refresh"]`. When labels get that specific, they become useless as filters -- you'd be filtering to a set of one. Put that detail in properties instead.

A few labels per node is the right target. Three to five is common. More than eight is usually a sign that something should be a property or an edge.

### Strategy 2: Source design

One source per data origin, not one per write. The source is the unit that earns or loses credibility over time. If you create a new source ID for every batch job or every request, credibility can never accumulate and the admission gate treats every write as neutral.

Good source IDs: `"slack:engineering"`, `"github:pr-reviews"`, `"user:alice"`, `"crawler:docs-site"`

Bad source IDs: `"write-123"`, `"batch-2024-06-01"`, `"import-job-abc"` -- these are too granular. The system can't learn that a source is reliable if every write invents a new one.

Think of a source as an actor with a reputation. Sources that are consistently right get higher credibility. Sources that contradict verified facts lose it. That only works if the same source ID is used consistently across writes from the same origin.

### Strategy 3: When to use edges vs labels vs namespaces

| Use case | Mechanism | Example |
|:---------|:----------|:--------|
| Hard isolation between tenants | Namespace | `db.Namespace("team-a", ...)` |
| Categorize for filtering | Labels | `Labels: []string{"security", "auth"}` |
| Track relationships | Edges | `edge(finding -> recommendation, type: "supports")` |
| Separate concerns in same tenant | Labels + edges | Security findings and code reviews share a namespace but have different labels and connect via edges |

Namespaces are for isolation. They're the right tool when two tenants should never see each other's data. They're the wrong tool for query routing. If you're creating a namespace per query type or per feature, you've probably hit a modeling problem -- use labels instead.

Edges express relationships between nodes. If you're tempted to encode a relationship in a label (like `"derived-from-finding-123"`), that's a signal to use an edge.

### Strategy 4: Write rich context, not fragments

The quality of your embeddings depends entirely on the quality of the text you write.

```go
// Bad: the embedding for "500 req/s" is nearly meaningless
ns.Write(ctx, client.WriteRequest{
    Content: "500 req/s",
})

// Good: self-contained claim with full context
ns.Write(ctx, client.WriteRequest{
    Content:    "The public API rate limit was raised to 500 req/s on March 15th",
    SourceID:   "config-service",
    Labels:     []string{"config", "api", "rate-limit"},
    Confidence: 0.95,
    ValidFrom:  time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
})
```

The embedding for `"500 req/s"` will match queries about velocity, bandwidth, and throughput almost equally. The embedding for `"The public API rate limit was raised to 500 req/s"` is specific to API configuration. The extra words aren't noise -- they're the context that makes the embedding meaningful.

Each node should be a complete, self-contained claim. If someone read only that node, they should understand exactly what it asserts.

### Strategy 5: Use ValidFrom for facts, not just ingestion time

`ValidFrom` is when the fact became true in the world, not when you wrote it to contextdb. These are different.

```go
// Your crawler discovers on March 20 that a change happened on March 15.
// Set ValidFrom to when it was TRUE, not when you learned it.
ns.Write(ctx, client.WriteRequest{
    Content:   "API rate limit is 500 req/s",
    ValidFrom: time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
    // TxTime is set automatically to now (March 20)
})
// "as of March 14" returns the old limit. "as of March 16" returns the new one.
```

When you skip `ValidFrom`, the system assumes the fact became true at ingestion time. For live data feeds that's often fine. For historical backfills, audit trails, or changelog imports, it will produce wrong answers on temporal queries.

`TxTime` (when you wrote the node) is always set automatically. You control `ValidFrom`. Set it whenever you know it.

### Strategy 6: Confidence is your prior, not your certainty

Confidence at write time is an estimate of how much to trust the claim based on what you know about the source. You don't need to be certain -- the scoring function and credibility system will refine it.

| Source | Starting confidence | Why |
|:-------|:-------------------|:----|
| Automated config service | 0.95 | Machine-generated, rarely wrong |
| Human-written docs | 0.80 | Usually accurate, sometimes stale |
| Slack conversation | 0.50 | Informal, might be wrong |
| Unverified user input | 0.30 | Needs validation |
| LLM-generated summary | 0.60 | Plausible but may hallucinate |

These are starting points. A source that consistently produces accurate claims will see its credibility rise and pull up the effective confidence of its nodes. A source that contradicts verified facts will have its credibility reduced. You just need a reasonable prior.

Setting confidence to `1.0` by default defeats the purpose. Setting it to `0.5` for everything means you've lost the signal entirely.

## Anti-patterns to avoid

- **One namespace per query type.** Namespaces are for isolation, not for query routing. Put related data together and use labels to filter.

- **Skipping source IDs.** Without a source, credibility can't accumulate and the admission gate treats every write as neutral. Always set `SourceID`.

- **Empty labels.** Labels are free and make retrieval dramatically better. Always add at least one, even if it's broad.

- **Giant text blobs.** Embeddings work best on focused, 1-3 sentence claims. If you're writing paragraphs, break them into individual claims and connect them with edges. A 500-word block of text will produce an embedding that's average over all of it -- no single query will score it highly.

- **Ignoring ValidFrom.** If you know when a fact became true, set it. Otherwise temporal queries assume "true since ingestion," which is often wrong for historical data, changelogs, or any fact you discovered after the fact.
