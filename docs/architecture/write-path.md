---
title: Write Path
parent: Architecture
nav_order: 2
---

# Write Path

Every write passes through the admission gate before being persisted.

## Sequence

```mermaid
sequenceDiagram
    participant App
    participant NS as NamespaceHandle
    participant SRC as Source Store
    participant VEC as Vector Index
    participant ADM as Admission Gate
    participant GR as Graph Store
    participant EL as Event Log

    App->>NS: Write(content, sourceID, vector)
    NS->>SRC: resolveSource(sourceID)
    SRC-->>NS: Source{credibility}

    alt has vector
        NS->>VEC: Search(vector, top=5)
        VEC-->>NS: nearest neighbours
    end

    NS->>ADM: Admit(candidate, source, neighbours)

    alt Rule 1: credibility < 0.05
        ADM-->>NS: REJECT (troll floor)
        NS-->>App: {admitted: false}
    else Rule 2: similarity >= 0.95
        ADM-->>NS: REJECT (near-duplicate)
        NS-->>App: {admitted: false}
    else Rule 3: cred * novelty < threshold
        ADM-->>NS: REJECT (below threshold)
        NS-->>App: {admitted: false}
    else all rules pass
        ADM-->>NS: ADMIT
        NS->>GR: UpsertNode(candidate)
        NS->>VEC: Index(vector entry)
        NS-->>App: {admitted: true, nodeID}
    end
```

## Step by step

### 1. Source resolution
Look up the source by `ExternalID`. If it doesn't exist, create one with neutral credibility (0.5). Apply label overrides ("moderator" -> 1.0, "troll" -> 0.05).

### 2. Near-duplicate scan
If the write includes a vector, do a quick ANN search for the 5 nearest existing nodes. This is used by the admission gate to detect duplicates and compute novelty.

### 3. Admission gate
Three rules run in order:

| Rule | Condition | Result |
|:-----|:----------|:-------|
| Credibility floor | `effective_credibility <= 0.05` | Reject |
| Near-duplicate | `max(similarity) >= 0.95` | Reject |
| Novelty threshold | `credibility * (1 - max_similarity) < threshold` | Reject |

### 4. Persist
If admitted:
- **Graph store**: `UpsertNode` writes the node with full metadata
- **Vector index**: `Index` adds the embedding for future ANN search
- **Metrics**: counters for admitted/rejected, latency histograms

### 5. Confidence assignment
The node's final confidence is:

```
node.confidence = initial_confidence * source_credibility
```

If no explicit confidence was provided, it defaults to the source's effective credibility.

## IngestText pipeline

`IngestText` adds LLM extraction before the standard write path:

```mermaid
sequenceDiagram
    participant App
    participant NS as NamespaceHandle
    participant EX as LLM Extractor
    participant ADM as Admission Gate
    participant GR as Graph Store

    App->>NS: IngestText("Alice knows Go and Python")
    NS->>EX: Extract(text)
    EX-->>NS: entities[Alice, Go, Python] + relations[knows, knows]

    loop for each entity
        NS->>ADM: Admit(entity_node)
        ADM-->>NS: admit/reject
        NS->>GR: UpsertNode (if admitted)
    end

    loop for each relation
        NS->>GR: UpsertEdge(src, dst, type)
    end

    NS-->>App: {nodes_written: 3, edges_written: 2}
```
