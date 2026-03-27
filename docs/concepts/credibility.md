---
title: Source Credibility
parent: Concepts
nav_order: 3
---

# Source Credibility

contextdb tracks the trustworthiness of information sources and uses it to gate admission and weight retrieval.

## How it works

```mermaid
flowchart TD
    A[New Claim Arrives] --> B{Source Known?}
    B -->|No| C[Create Source<br/>credibility = 0.5]
    B -->|Yes| D[Load Source]
    C --> E{Check Labels}
    D --> E
    E -->|moderator/admin| F[Credibility = 1.0]
    E -->|troll/flagged| G[Credibility = 0.05]
    E -->|no override| H[Use Stored Score]
    F --> I{Admission Gate}
    G --> I
    H --> I
    I -->|Rule 1: cred < 0.05| J[REJECT<br/>Troll floor]
    I -->|Rule 2: sim >= 0.95| K[REJECT<br/>Near-duplicate]
    I -->|Rule 3: cred * novelty < threshold| L[REJECT<br/>Below threshold]
    I -->|All rules pass| M[ADMIT]

    style J fill:#e74c3c,stroke:#333,color:#fff
    style K fill:#e67e22,stroke:#333,color:#fff
    style L fill:#e67e22,stroke:#333,color:#fff
    style M fill:#2ecc71,stroke:#333,color:#fff
```

```mermaid
graph LR
    subgraph "Beta Distribution Evolution"
        direction TB
        A["New Source<br/>Beta(1,1)<br/>Credibility: 0.50"]
        B["After 3 validated claims<br/>Beta(4,1)<br/>Credibility: 0.80"]
        C["After 1 refuted claim<br/>Beta(4,2)<br/>Credibility: 0.67"]
        D["After 5 more validated<br/>Beta(9,2)<br/>Credibility: 0.82"]
    end
    A -->|"3 validated"| B
    B -->|"1 refuted"| C
    C -->|"5 validated"| D

    style A fill:#95a5a6,stroke:#333,color:#fff
    style B fill:#2ecc71,stroke:#333,color:#fff
    style C fill:#e67e22,stroke:#333,color:#fff
    style D fill:#27ae60,stroke:#333,color:#fff
```

## Sources

Every piece of data has a source. Sources are automatically created on first write:

```go
// First write from "user:bob" creates a source with credibility 0.5
ns.Write(ctx, client.WriteRequest{
    Content:  "Go is fast",
    SourceID: "user:bob",
    Vector:   embedding,
})
```

Source fields:

| Field | Description |
|:------|:------------|
| `ExternalID` | Your identifier (Discord ID, agent name, URL) |
| `CredibilityScore` | Base credibility [0, 1], starts at 0.5 |
| `Labels` | Override labels: "moderator", "admin", "troll", "flagged" |
| `ClaimsAsserted` | Total claims from this source |
| `ClaimsValidated` | Claims later confirmed |
| `ClaimsRefuted` | Claims later contradicted |

## Label overrides

Labels override the numeric score entirely:

```go
// Full trust: credibility always 1.0
ns.LabelSource(ctx, "moderator:alice", []string{"moderator"})

// Blocked: credibility always 0.05, all writes rejected
ns.LabelSource(ctx, "user:spammer", []string{"troll"})
```

| Label | Effective Credibility |
|:------|:---------------------|
| `moderator` | 1.0 |
| `admin` | 1.0 |
| `flagged` | 0.05 |
| `troll` | 0.05 |

## The admission gate

Three rules run in order on every write:

### Rule 1: Credibility floor
Sources with effective credibility <= 0.05 are always rejected. This stops troll floods at the gate.

### Rule 2: Near-duplicate detection
If an existing node has cosine similarity >= 0.95 to the candidate, the write is rejected as a duplicate.

### Rule 3: Novelty threshold
The combined score `credibility * novelty` must exceed the namespace's admission threshold:

| Namespace | Threshold | Effect |
|:----------|:----------|:-------|
| belief_system | 0.15 | Low bar. Credibility gates retrieval instead |
| general | 0.25 | Balanced |
| agent_memory | 0.35 | Stricter. Avoids low-value episodes |
| procedural | 0.40 | Only well-established procedures admitted |

## Bayesian credibility learning

Sources use a [Beta distribution](https://en.wikipedia.org/wiki/Beta_distribution) to model credibility:

- **Alpha** = 1 + validated claims (evidence *for* trustworthiness)
- **Beta** = 1 + refuted claims (evidence *against*)
- **Credibility** = Alpha / (Alpha + Beta)

New sources start at Beta(1,1) — a uniform prior meaning "we know nothing." Each validation or refutation shifts the distribution. The more observations, the more confident the estimate.

This is mathematically principled: 1000 validated claims from a source that then gets one wrong doesn't crash its credibility to zero. The Beta distribution naturally handles this — it becomes a small dip in a well-established track record.

{: .note }
> **How this compares**: Most systems use static trust scores (0-100 set by an admin) or binary allow/deny lists. contextdb's Bayesian approach means credibility is *learned from evidence* — no manual tuning required. A new source starts neutral and earns or loses trust based on how its claims hold up over time.

## Domain-scoped credibility

A source can be credible in one domain and unreliable in another. `standup_notes` is highly credible for project status but less so for technical correctness.

```go
// Source credibility varies by domain
cred := source.DomainCredibility("project-status")  // 0.92
cred = source.DomainCredibility("security")          // 0.45
cred = source.DomainCredibility("")                   // 0.68 (global fallback)
```

Domain credibility is tracked per (source, domain) pair. When no domain-specific data exists, the global credibility is used as a fallback.

## Confidence propagation

When a write is admitted, the node's confidence is:

```
node.confidence = source_credibility * confidence_multiplier
```

This means a moderator's claims (credibility 1.0) carry full confidence, while unknown sources (credibility 0.5) are automatically discounted.
