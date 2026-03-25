# contextdb

A general-purpose temporal graph-vector database built in Go.

Stores **claims, facts, memories, and beliefs** as nodes in a graph. Every
stored item carries an embedding vector, a temporal validity window, a
confidence score, and a provenance chain. Retrieval is a weighted scoring
function over all four dimensions simultaneously. The caller supplies the
weights.

## Core properties

- **Bi-temporal storage** — every node tracks `valid_time` (when the fact
  was true in the world) and `transaction_time` (when the system learned it)
  independently. Point-in-time queries are first-class.
- **Credibility-weighted retrieval** — sources carry credibility scores that
  propagate through endorsement edges. Troll-flood and poisoning attacks are
  mitigated at admission time, not post-hoc.
- **Caller-supplied scoring strategy** — similarity, confidence, recency, and
  utility weights are query parameters, not database constants. Different
  namespace modes (belief system, agent memory, procedural) ship sensible
  defaults.
- **Pluggable backends** — `GraphStore`, `VectorIndex`, `KVStore`, and
  `EventLog` are interfaces. Ship three profiles from one binary: embedded
  (BadgerDB + in-process HNSW), standard (Postgres + pgvector), scaled
  (Postgres + Qdrant + Redis).
- **Schema-agnostic** — caller-defined labels, properties, and edge types.
  The database does not interpret content.

## Scoring function

```
score(candidate) =
    w_sim  * cosine_similarity(candidate.vector, query.vector)
  + w_conf * candidate.confidence
  + w_rec  * exp(-alpha * age_hours)
  + w_util * utility_feedback_score
```

All weights are normalised at query time. Caller supplies `alpha` (decay
rate) and the four weights via `core.ScoreParams`.

## Namespace modes

| Mode | Best for | Key weight |
|---|---|---|
| `belief_system` | Channel bots, fact tracking, poisoning resistance | confidence |
| `agent_memory` | Agentic workflows with task outcome feedback | utility + recency |
| `general` | Balanced RAG, document retrieval | similarity |
| `procedural` | Skill / workflow storage | confidence, slow decay |

## Quick start

```bash
# Run the embedded demo (no external dependencies)
make run

# Run all tests
make test-verbose

# Run bench visualisations (ASCII + HTML report)
make bench
# open /tmp/contextdb_bench.html

# Test coverage summary
make cover-text
```

## Docker

```bash
# Build and run locally
docker compose up --build

# Or build the image directly
docker build -t contextdb:dev .
docker run --rm contextdb:dev
```

The CI workflow builds, tests, and pushes to `ghcr.io/<owner>/contextdb`
on every push to `main`.

## Project layout

```
contextdb/
├── cmd/contextdb/        # binary entrypoint
├── internal/
│   ├── core/             # domain types: Node, Edge, Source, ScoreParams
│   ├── store/            # GraphStore, VectorIndex, KVStore, EventLog interfaces
│   │   └── memory/       # in-process implementations (Phase 0)
│   ├── ingest/           # write path: extraction, admission, conflict detection
│   ├── retrieval/        # read path: concurrent fan-out, fusion, scoring
│   └── namespace/        # namespace config and mode presets
├── bench/                # score visualisation tests (ASCII + HTML)
├── pkg/client/           # Go SDK (Phase 5)
└── .github/workflows/    # CI: test → build → docker push to ghcr.io
```

## Roadmap

- **Phase 0** ✅ — core types, in-memory stores, scoring function, tests
- **Phase 1** — embedded BadgerDB backend + in-process HNSW vector index
- **Phase 2** — Postgres backend (pgvector + recursive CTE graph)
- **Phase 3** — ingest pipeline with LLM entity extraction
- **Phase 4** — RAPTOR compaction worker
- **Phase 5** — gRPC + REST API, multi-tenancy, OTel tracing
- **Phase 6** — LongMemEval harness, Helm chart, Go SDK

## Related work

- [Zep / Graphiti](https://arxiv.org/abs/2501.13956) — bi-temporal KG for agent memory
- [Hindsight](https://arxiv.org/abs/2512.12818) — TEMPR multi-strategy retrieval
- [RAPTOR](https://arxiv.org/abs/2401.18059) — hierarchical summarisation for compaction
- [A-MAC](https://arxiv.org/abs/2603.04549) — adaptive memory admission control
