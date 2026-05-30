---
title: GraphQL API
---

# GraphQL API

contextdb exposes GraphQL on the REST listener at `/graphql`.

## Search

```bash
curl -X POST http://localhost:7701/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ search(namespace: \"my-app\", query: \"routing patterns\", limit: 5) { totalCount nodes { id content score scoreBreakdown { similarity confidence recency utility } sources { name effectiveCredibility } } } }"
  }'
```

`search` accepts:

| Argument | Type | Default | Description |
|:---------|:-----|:--------|:------------|
| `namespace` | string | `default` | Namespace to search |
| `mode` | string | `general` | Namespace mode used when opening the namespace |
| `query` | string | required | Text query |
| `filter` | `FilterInput` | none | Optional graph/source/time filters |
| `limit` | int | `10` | Maximum nodes returned |

GraphQL uses normal retrieval when the server has embeddings available. In embedded/no-embedder setups it falls back to a content scan so the endpoint remains useful for local inspection.

## Introspection

```graphql
{
  version {
    version
    apiVersion
    docsVersion
    compatibility
    latestMigration
    releaseNotesPath
    features { name status since }
    migrations { version name }
  }

  features { name status since description }
  migrations { version name }
}
```

The `version` query returns the release summary for the live server. `features` and `migrations` are available as top-level queries when clients only need one list.

## Filters

```graphql
{
  search(
    namespace: "my-app"
    query: "rate limit"
    filter: {
      contentContains: "API"
      sourceCredibilityMin: 0.7
      edgeTypes: ["supports"]
      edgeWeightMin: 0.5
    }
  ) {
    totalCount
    nodes {
      id
      content
      credibility
      edges { type weight to { id content } }
      sources { name effectiveCredibility }
    }
  }
}
```

Supported filter fields include `and`, `or`, `not`, `contentContains`, `similarityMin`, `hasEdgeTo`, `edgeTypes`, `edgeWeightMin`, `sourceCredibilityMin`, `sourceCredibilityMax`, `validAt`, `validFromBefore`, and `validToAfter`.

## Narrative And Gaps

```graphql
query {
  narrative(namespace: "my-app", nodeId: "550e8400-e29b-41d4-a716-446655440000") {
    summary
    claim { text confidence sourceId }
    evidence { text relation }
    contradictions { text relation }
    confidenceExplanation
  }

  knowledgeGaps(namespace: "my-app", topK: 20, minGapSize: 0.5, maxGaps: 10) {
    coverageScore
    totalNodes
    gaps {
      nearestTopics
      densityScore
      temporalGapSeconds
    }
  }
}
```

## Feedback Mutations

```graphql
mutation {
  validateClaim(namespace: "my-app", nodeId: "550e8400-e29b-41d4-a716-446655440000") {
    nodeId
    action
    confidence
    sourceCredibility
  }
}
```

Available mutations:

| Mutation | Effect |
|:---------|:-------|
| `validateClaim` | Increases claim confidence and validates the asserting source |
| `refuteClaim` | Sets claim confidence low and refutes the asserting source |
| `markUseful` | Increases utility and updates SM-2 recall metadata |
| `markStale` | Decreases confidence and utility |

## Feedback Events

```graphql
query {
  feedbackEvents(namespace: "my-app", after: "2026-05-30T00:00:00Z") {
    eventId
    nodeId
    nodeVersion
    action
    sourceId
    sourceCredibility
    txTime
  }
}
```

Feedback events are durable audit records emitted by `validateClaim`, `refuteClaim`, `markUseful`, and `markStale`.
