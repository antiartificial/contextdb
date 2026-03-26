---
title: Write Path
parent: Architecture
nav_order: 2
---

# Write Path

Every write passes through auto-embedding, the admission gate, and conflict detection before being persisted.

## Sequence

```mermaid
sequenceDiagram
    participant App
    participant NS as NamespaceHandle
    participant EMB as Embedder
    participant SRC as Source Store
    participant VEC as Vector Index
    participant ADM as Admission Gate
    participant CD as Conflict Detector
    participant GR as Graph Store
    participant EL as Event Log

    App->>NS: Write(content, sourceID, vector?)

    alt no vector provided & embedder configured
        NS->>EMB: Embed(content)
        EMB-->>NS: vector
    end

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
        NS->>CD: Detect(candidate, neighbours)
        CD-->>NS: ConflictIDs
        NS->>GR: UpsertNode(candidate)
        NS->>VEC: Index(vector entry)
        NS->>EL: Append(event)
        NS-->>App: {admitted: true, nodeID, conflictIDs}
    end
```

## Step by step

### 1. Auto-embedding

If the write does not include a pre-computed vector and an `Embedder` is configured, the `Content` text is automatically embedded. The embedding is cached (LRU with SHA256 keys) to avoid redundant API calls. See [Auto-Embedding](embedding) for details.

### 2. Source resolution

Look up the source by `ExternalID`. If it doesn't exist, create one with neutral credibility (0.5). Apply label overrides ("moderator" -> 1.0, "troll" -> 0.05).

### 3. Near-duplicate scan

If the write includes a vector, do a quick ANN search for the 5 nearest existing nodes. This is used by the admission gate to detect duplicates and compute novelty.

### 4. Admission gate

Three rules run in order:

| Rule | Condition | Result |
|:-----|:----------|:-------|
| Credibility floor | `effective_credibility <= 0.05` | Reject |
| Near-duplicate | `max(similarity) >= 0.95` | Reject |
| Novelty threshold | `credibility * (1 - max_similarity) < threshold` | Reject |

### 5. Conflict detection

After admission, the conflict detector examines the nearest neighbours for contradictions. Candidates with moderate similarity (0.3–0.95) and shared labels are assessed. If an LLM provider is configured, it evaluates contradiction probability; otherwise, a heuristic is used. Confirmed contradictions create `contradicts` edges in the graph. See [Conflict Detection](../concepts/conflict-detection) for details.

### 6. Persist

If admitted:
- **Graph store**: `UpsertNode` writes the node with full metadata
- **Vector index**: `Index` adds the embedding for future ANN search
- **Event log**: `Append` records the write event
- **Metrics**: counters for admitted/rejected, latency histograms

### 7. Confidence assignment

The node's final confidence is:

```
node.confidence = initial_confidence * source_credibility
```

If no explicit confidence was provided, it defaults to the source's effective credibility.

### 8. Credibility feedback

The [credibility learning](../concepts/conflict-detection#credibility-feedback-loop) background worker periodically reviews contradiction edges and adjusts source credibility via Bayesian updates. Sources that consistently produce validated information gain trust; sources that are frequently contradicted lose it.

## IngestText pipeline

`IngestText` adds LLM extraction before the standard write path:

```mermaid
sequenceDiagram
    participant App
    participant NS as NamespaceHandle
    participant EX as LLM Extractor
    participant EMB as Embedder
    participant ADM as Admission Gate
    participant GR as Graph Store

    App->>NS: IngestText("Alice knows Go and Python")
    NS->>EX: Extract(text)
    EX-->>NS: entities[Alice, Go, Python] + relations[knows, knows]

    loop for each entity
        opt embedder configured
            NS->>EMB: Embed(entity.content)
            EMB-->>NS: vector
        end
        NS->>ADM: Admit(entity_node)
        ADM-->>NS: admit/reject
        NS->>GR: UpsertNode (if admitted)
    end

    loop for each relation
        NS->>GR: UpsertEdge(src, dst, type)
    end

    NS-->>App: {nodes_written: 3, edges_written: 2}
```
