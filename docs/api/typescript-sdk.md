---
title: TypeScript SDK
parent: API Reference
nav_order: 5
---

# TypeScript SDK

The TypeScript SDK is a REST client for contextdb, located in `sdk/typescript/`.

## Installation

```bash
npm install contextdb
```

## Connection

```typescript
import { ContextDB } from "contextdb";

const db = new ContextDB("http://localhost:7701");
```

## Namespace

```typescript
const ns = db.namespace("my-app", "general");
```

The mode parameter accepts: `general`, `belief_system`, `agent_memory`, `procedural`.

## Write

```typescript
const result = await ns.write({
  content: "Go 1.22 added routing patterns to net/http",
  sourceId: "docs-crawler",
  labels: ["Claim"],
  properties: { url: "https://go.dev/blog" },
  confidence: 0.9,
});

console.log(result.nodeId);     // UUID string
console.log(result.admitted);   // true/false
console.log(result.reason);     // rejection reason if not admitted
console.log(result.conflictIds); // contradicting node IDs
```

### WriteRequest fields

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `content` | `string` | Yes | Text content |
| `sourceId` | `string` | Yes | External source identifier |
| `labels` | `string[]` | No | Node labels |
| `properties` | `Record<string, unknown>` | No | Arbitrary metadata |
| `vector` | `number[]` | No | Pre-computed embedding |
| `modelId` | `string` | No | Embedding model identifier |
| `confidence` | `number` | No | Confidence [0, 1] |

### WriteResult fields

| Field | Type | Description |
|:------|:-----|:------------|
| `nodeId` | `string` | UUID of the written node |
| `admitted` | `boolean` | Whether the write was accepted |
| `reason` | `string` | Rejection reason (empty if admitted) |
| `conflictIds` | `string[]` | UUIDs of contradicting nodes |

## Retrieve

```typescript
// Vector-based query
const results = await ns.retrieve({ vector: [0.1, 0.2, 0.3], topK: 5 });

// Text-based query (auto-embedded server-side)
const results = await ns.retrieve({ text: "What changed in Go 1.22?", topK: 5 });

// With label filtering
const results = await ns.retrieve({
  text: "routing patterns",
  labels: ["Claim"],
  topK: 10,
});

// With custom score weights
const results = await ns.retrieve({
  text: "routing patterns",
  topK: 5,
  scoreParams: {
    similarityWeight: 0.6,
    confidenceWeight: 0.2,
    recencyWeight: 0.1,
    utilityWeight: 0.1,
  },
});

for (const r of results) {
  console.log(`${r.score.toFixed(2)} ${r.node.properties.text}`);
}
```

### RetrieveRequest fields

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `vector` | `number[]` | For vector search | Query embedding |
| `text` | `string` | For text search | Auto-embedded server-side |
| `seedIds` | `string[]` | For graph walk | Known relevant node IDs |
| `topK` | `number` | No | Max results (default: 10) |
| `labels` | `string[]` | No | Filter to nodes with all specified labels |
| `scoreParams` | `ScoreParams` | No | Override scoring weights |

### Result fields

| Field | Type | Description |
|:------|:-----|:------------|
| `node` | `Node` | Full node with labels, properties, metadata |
| `score` | `number` | Composite score [0, 1] |
| `similarityScore` | `number` | Vector similarity component |
| `confidenceScore` | `number` | Confidence component |
| `recencyScore` | `number` | Recency component |
| `utilityScore` | `number` | Utility component |
| `retrievalSource` | `string` | "vector", "graph", or "fused" |

## Ingest text

```typescript
const result = await ns.ingestText(
  "Alice knows Go and Python. Bob specializes in Rust.",
  "docs-crawler",
);

console.log(result.nodesWritten); // 4
console.log(result.edgesWritten); // 3
console.log(result.rejected);     // 0
```

## Label source

```typescript
await ns.labelSource("user:spammer", ["troll"]);
await ns.labelSource("moderator:alice", ["moderator"]);
```

## Health check

```typescript
await db.ping();   // { status: "ok" }
await db.stats();  // server statistics object
```
