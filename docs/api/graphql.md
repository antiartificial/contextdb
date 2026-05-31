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

## Source Trust Timeline

```graphql
query {
  sourceTrustTimeline(namespace: "my-app", sourceId: "docs-crawler") {
    nodeId
    action
    sourceCredibility
    txTime
  }
}
```

The timeline is derived from feedback events that changed the source's effective credibility.

## Explain Rank

```graphql
query {
  explainRank(
    namespace: "my-app"
    nodeId: "550e8400-e29b-41d4-a716-446655440000"
    otherNodeId: "660e8400-e29b-41d4-a716-446655440001"
  ) {
    winnerNodeId
    loserNodeId
    margin
    summary
    node {
      nodeId
      score
      scoreBreakdown { similarity confidence recency utility }
      evidence {
        compoundConfidence
        supportCount
        links { nodeId edgeId edgeWeight confidence text }
      }
    }
    other {
      nodeId
      score
      scoreBreakdown { similarity confidence recency utility }
      evidence { compoundConfidence supportCount }
    }
    factors { factor nodeContribution otherContribution delta }
  }
}
```

`explainRank` compares two nodes under the namespace scoring model and returns component deltas plus supporting graph evidence when `supports` chains are available.

## Claim Review Queue

```graphql
query {
  reviewQueue(
    namespace: "my-app"
    lowConfidenceThreshold: 0.35
    sourceTrustDropThreshold: 0.2
    sourceRefutationThreshold: 2
    escalationAfterHours: 72
    sourceAnomalyEscalationPriority: 0.9
    types: ["source_trust_anomaly"]
    sourceId: "docs-crawler"
    status: "open"
    limit: 20
  ) {
    id
    type
    priority
    reason
    nodeId
    nodeIds
    sourceId
    action
    text
    createdAt
    suggestedAction
    confidence
    status
    owner
    decision
    note
    reviewedAt
    escalated
    escalationLevel
    escalationReason
    escalationAgeHours
  }
}
```

The queue derives review tasks from refuted and stale feedback, low-confidence active claims, contradiction clusters, and configured source-trust anomalies such as credibility drops or repeated refutations.

Filter arguments are additive: `types` narrows by review item type, `sourceId` focuses source-specific tasks, and `status`/`owner` focus the latest workflow state. Items with no recorded decision match `status: "open"`.

Use `reviewEscalationDigest` for grouped escalation counts:

```graphql
query {
  reviewEscalationDigest(namespace: "my-app", escalationAfterHours: 72) {
    totalEscalated
    groups {
      owner
      sourceId
      type
      escalationLevel
      count
      maxPriority
      maxAgeHours
      reviewIds
    }
  }
}
```

Record and list durable escalation digest snapshots for handoffs:

```graphql
mutation {
  recordReviewEscalationDigest(namespace: "my-app", escalationAfterHours: 72, note: "weekly review handoff") {
    totalEscalated
    note
  }
}

query {
  reviewEscalationDigests(namespace: "my-app") {
    note
    totalEscalated
    groups { owner count escalationLevel }
  }

  reviewHandoffs(namespace: "my-app", owner: "alice", escalationLevel: "review_overdue") {
    note
    totalEscalated
    groups { owner count escalationLevel }
  }

  reviewHandoffWebhookPlan(
    namespace: "my-app"
    owner: "alice"
    escalationLevel: "review_overdue"
    targetUrl: "https://ops.example.test/contextdb/handoffs"
    secret: "webhook-signing-secret"
  ) {
    targetUrl
    dryRun
    payloadSha256
    signature
    maxAttempts
  }
}
```

`reviewHandoffWebhookPlan` is dry-run only: it prepares signed delivery payloads and retry metadata without sending outbound webhooks.

Execute a handoff webhook with an explicit mutation:

```graphql
mutation {
  deliverReviewHandoffWebhook(
    namespace: "my-app"
    owner: "alice"
    escalationLevel: "review_overdue"
    targetUrl: "https://ops.example.test/contextdb/handoffs"
    secret: "webhook-signing-secret"
    execute: true
    timeoutMs: 5000
  ) {
    targetUrl
    dryRun
    executed
    statusCode
    responseBody
    error
  }
}
```

`execute: true` is required. Delivery is synchronous and captures the response without scheduling background retries.

List durable delivery receipts with:

```graphql
query {
  reviewHandoffDeliveryReceipts(namespace: "my-app") {
    digestEventId
    targetUrl
    success
    statusCode
    payloadSha256
    responseSha256
    error
  }
}
```

Find unresolved failed deliveries without sending retries:

```graphql
query {
  reviewHandoffRetryCandidates(namespace: "my-app") {
    digestEventId
    targetUrl
    attempts
    lastStatusCode
    payloadSha256
    lastError
  }
}
```

List retry pacing recommendations without sending retries:

```graphql
query {
  reviewHandoffRetryRecommendations(namespace: "my-app") {
    digestEventId
    targetUrl
    attempts
    recommendedAfter
    delaySeconds
    ready
    reason
  }
}
```

Recommendations add read-only backoff guidance to retry candidates. They do not schedule or send retries.

Retry one unresolved failed delivery explicitly:

```graphql
mutation {
  retryReviewHandoffWebhook(
    namespace: "my-app"
    digestEventId: "550e8400-e29b-41d4-a716-446655440000"
    targetUrl: "https://ops.example.test/contextdb/handoffs"
    secret: "webhook-signing-secret"
    execute: true
    timeoutMs: 5000
  ) {
    targetUrl
    executed
    statusCode
    responseBody
    error
  }
}
```

Retry execution uses the digest event ID and target URL from `reviewHandoffRetryCandidates`, requires `execute: true`, and records a new delivery receipt.

Record and inspect review workflow state with:

```graphql
mutation {
  recordReviewDecision(
    namespace: "my-app"
    reviewId: "low_confidence:550e8400-e29b-41d4-a716-446655440000"
    status: "assigned"
    owner: "alice"
    decision: "needs_evidence"
    note: "check source logs"
  ) {
    reviewId
    status
    owner
    decision
  }
}
```

```graphql
query {
  reviewDecisions(namespace: "my-app") {
    reviewId
    status
    owner
    decision
    note
    recheckAt
    txTime
  }
}
```

## Knowledge Acquisition Plan

```graphql
query {
  acquisitionPlan(namespace: "my-app", budget: 5, maxGaps: 3) {
    namespace
    coverageScore
    totalNodes
    tasks {
      id
      type
      priority
      description
      prompt
      relatedNodeIds
      nearestTopics
    }
  }
}
```

The plan converts knowledge gaps and weak-claim signals into prioritized research, crawl, verification, and refresh tasks.
