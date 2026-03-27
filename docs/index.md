---
title: Home
layout: home
nav_order: 1
---

# contextdb

**The epistemics layer for AI systems — memory that knows what it knows, what it doesn't, and why it believes what it does.**
{: .fs-6 .fw-300 }

contextdb stores claims, facts, memories, and beliefs as nodes in a graph. Every item carries an embedding vector, a temporal validity window, a confidence score, and a provenance chain. Retrieval scores across all four dimensions simultaneously. The caller supplies the weights.

[Get started]({{ site.baseurl }}/quick-start){: .btn .btn-primary .fs-5 .mb-4 .mb-md-0 .mr-2 }
[View on GitHub](https://github.com/antiartificial/contextdb){: .btn .fs-5 .mb-4 .mb-md-0 }

<div class="stat-row">
  <div class="stat-box">
    <span class="stat-value">4</span>
    <span class="stat-label">time dimensions</span>
  </div>
  <div class="stat-box">
    <span class="stat-value">4</span>
    <span class="stat-label">score components</span>
  </div>
  <div class="stat-box">
    <span class="stat-value">4</span>
    <span class="stat-label">namespace modes</span>
  </div>
  <div class="stat-box">
    <span class="stat-value">4</span>
    <span class="stat-label">deployment modes</span>
  </div>
</div>

---

## Why contextdb?

Most vector databases treat embeddings as the whole story. But AI systems that interact with the real world need more:

<div class="feature-grid">
<div class="feature-card" markdown="1">
<span class="feature-icon"><i class="fa-solid fa-clock-rotate-left"></i></span>
### Facts expire
contextdb tracks `valid_time` (when the fact was true) and `transaction_time` (when the system learned it) independently. Point-in-time queries are first-class.
</div>

<div class="feature-card" markdown="1">
<span class="feature-icon"><i class="fa-solid fa-shield-halved"></i></span>
### Sources lie
Source credibility is tracked via Bayesian updates and used as an admission gate. Sources that produce validated info gain trust; those that contradict reliable facts lose it.
</div>

<div class="feature-card" markdown="1">
<span class="feature-icon"><i class="fa-solid fa-chart-line-down"></i></span>
### Memory decays
Different knowledge decays at different rates. Episodic memories fade in hours; procedural skills persist for months. Background workers consolidate episodic into durable semantic knowledge.
</div>

<div class="feature-card" markdown="1">
<span class="feature-icon"><i class="fa-solid fa-code-compare"></i></span>
### Contradictions happen
Conflicting claims are detected at write time, tracked as graph edges, and accounted for in retrieval scoring. The system knows what it disagrees about.
</div>
</div>

## Five lines to a working database

Zero external dependencies. No Docker. No config files. One `go get` and you're running. Auto-embedding lets you skip the vector. Just send text.

<div class="code-tabs">
<div class="tab-buttons">
  <button class="tab-btn active" data-tab="go"><i class="fa-brands fa-golang"></i> Go</button>
  <button class="tab-btn" data-tab="python"><i class="fa-brands fa-python"></i> Python</button>
  <button class="tab-btn" data-tab="typescript"><i class="fa-brands fa-js"></i> TypeScript</button>
  <button class="tab-btn" data-tab="curl"><i class="fa-solid fa-terminal"></i> curl</button>
</div>

<div class="tab-content active" data-lang="go">
<div class="language-go highlighter-rouge"><div class="highlight"><pre class="highlight"><code>db := client.MustOpen(client.Options{})
defer db.Close()

ns := db.Namespace("my-app", namespace.ModeGeneral)
ns.Write(ctx, client.WriteRequest{
    Content:  "Go 1.22 added routing patterns to net/http",
    SourceID: "docs-crawler",
})
results, _ := ns.Retrieve(ctx, client.RetrieveRequest{
    Text: "What changed in Go 1.22?", TopK: 5,
})</code></pre></div></div>
</div>

<div class="tab-content" data-lang="python">
<div class="language-python highlighter-rouge"><div class="highlight"><pre class="highlight"><code>from contextdb import ContextDB

db = ContextDB("http://localhost:7701")
ns = db.namespace("my-app", mode="general")
ns.write(content="Go 1.22 added routing patterns", source_id="docs-crawler")
results = ns.retrieve(text="What changed in Go 1.22?", top_k=5)</code></pre></div></div>
</div>

<div class="tab-content" data-lang="typescript">
<div class="language-typescript highlighter-rouge"><div class="highlight"><pre class="highlight"><code>import { ContextDB } from "contextdb";

const db = new ContextDB("http://localhost:7701");
const ns = db.namespace("my-app", "general");
await ns.write({ content: "Go 1.22 added routing patterns", sourceId: "docs-crawler" });
const results = await ns.retrieve({ text: "What changed in Go 1.22?", topK: 5 });</code></pre></div></div>
</div>

<div class="tab-content" data-lang="curl">
<div class="language-bash highlighter-rouge"><div class="highlight"><pre class="highlight"><code># Write
curl -X POST http://localhost:7701/v1/namespaces/my-app/write \
  -H "Content-Type: application/json" \
  -d '{"content": "Go 1.22 added routing patterns", "source_id": "docs-crawler"}'

# Retrieve
curl -X POST http://localhost:7701/v1/namespaces/my-app/retrieve \
  -H "Content-Type: application/json" \
  -d '{"text": "What changed in Go 1.22?", "top_k": 5}'</code></pre></div></div>
</div>
</div>

## Architecture at a glance

```mermaid
graph LR
    subgraph Client SDKs
        GO[Go SDK]
        PY[Python SDK]
        TS[TypeScript SDK]
    end

    subgraph Server
        GRPC[gRPC :7700]
        REST[REST :7701]
        OBS[Observe :7702]
        ADMIN[Admin UI]
    end

    subgraph Core
        EMB[Auto-Embedding]
        ING[Ingest + Conflict Detection]
        RET[Retrieval + Reranking]
        BG[Consolidation + Recall]
    end

    subgraph Backends
        MEM[Memory]
        BAD[BadgerDB + HNSW]
        PG[Postgres + pgvector]
        QD[Qdrant]
        RD[Redis]
    end

    GO --> GRPC
    PY --> REST
    TS --> REST
    GRPC --> EMB
    REST --> EMB
    EMB --> ING
    EMB --> RET
    ING --> MEM & BAD & PG & QD & RD
    RET --> MEM & BAD & PG & QD & RD
    BG --> MEM & BAD & PG & QD & RD

    style GRPC fill:#4a9eff,stroke:#333,color:#fff
    style REST fill:#4a9eff,stroke:#333,color:#fff
    style MEM fill:#2ecc71,stroke:#333,color:#fff
    style BAD fill:#27ae60,stroke:#333,color:#fff
    style PG fill:#16a085,stroke:#333,color:#fff
    style QD fill:#8e44ad,stroke:#333,color:#fff
    style RD fill:#c0392b,stroke:#333,color:#fff
```

## The scoring function

Every retrieval result is scored by a weighted combination of four dimensions:

```
score = w_sim  * cosine_similarity(candidate, query)
      + w_conf * confidence
      + w_rec  * exp(-alpha * age_hours)
      + w_util * utility_feedback
```

All weights are normalised at query time. You supply `alpha` (decay rate) and the four weights, or use namespace mode defaults.

## Deployment modes

| Mode | Backend | Use case |
|:-----|:--------|:---------|
| **Embedded** | In-memory or BadgerDB | Development, testing, sidecars, CLIs |
| **Standard** | Postgres + pgvector | Production single-node, teams |
| **Remote** | gRPC to contextdb server | Microservices, multi-language clients |
| **Scaled** | Qdrant + Redis + Postgres | High-throughput production |

## Namespace modes

| Mode | Best for | Key weight |
|:-----|:---------|:-----------|
| `belief_system` | Fact tracking, poisoning resistance | confidence |
| `agent_memory` | Agentic workflows with task feedback | utility + recency |
| `general` | Balanced RAG, document retrieval | similarity |
| `procedural` | Skill and workflow storage | confidence, slow decay |

## Key features

| Feature | Description |
|:--------|:------------|
| [Auto-embedding](architecture/embedding) | Text automatically embedded via OpenAI, local, or custom providers with LRU cache |
| [Conflict detection](concepts/conflict-detection) | Near-duplicate scan, contradiction tracking, `contradicts` edges |
| [Credibility learning](concepts/credibility) | Bayesian source trust updates based on validation/refutation |
| [Reranking](architecture/read-path) | Optional LLM cross-encoder reranking after fusion |
| [Label filtering](api/go-sdk) | Filter retrieval by node labels |
| [Background workers](architecture/background-workers) | Memory consolidation and active recall |
| [RBAC](concepts/rbac) | Token-based read/write/admin permissions per tenant |
| [Snapshot/restore](api/go-sdk#export--import) | NDJSON namespace export and import |
| [Python SDK](api/python-sdk) | REST client for Python applications |
| [TypeScript SDK](api/typescript-sdk) | REST client for TypeScript/Node.js applications |
| [Scaled deployment](deployment/scaled) | Qdrant + Redis + Postgres for high throughput |
| [Benchmarks](benchmarks) | MTEB, adversarial, LongMemEval, fitness suite |
| [Admin UI](deployment/scaled) | Built-in dashboard on observe port |

## Epistemics layer

Features that make AI memory auditable and trustworthy:

| Feature | Description |
|:--------|:------------|
| [Belief reconciliation](concepts/belief-reconciliation) | Structured disagreements between agents with evidence chains — "git diff for beliefs" |
| [Narrative retrieval](concepts/narrative-retrieval) | "Walk me through what you know about X and why" with full citations |
| [Knowledge gap detection](concepts/knowledge-gaps) | "What don't I know?" — sparse region detection with acquisition suggestions |
| [Calibration pipeline](concepts/calibration) | Brier score, ECE, Platt scaling — confidence becomes calibrated probability |
| [GDPR erasure](concepts/gdpr) | Audit-trailed right-to-erasure across graph, vectors, KV, and event log |
| [Interference detection](concepts/interference) | Low-credibility sources can't erode well-established claims |
| [Claim federation](concepts/federation) | Gossip-based multi-instance replication with Beta-space credibility merge |
| [Cascade retraction](concepts/retraction) | Non-destructive "I take this back" that cascades through derived claims |
| [Active learning](concepts/active-learning) | System recommends what information to acquire next |
| [Query DSL](api/dsl) | Pipe syntax and CQL with temporal, graph, and weight clauses |

---

Built with Go. No CGO. Single binary. Scratch Docker image.
