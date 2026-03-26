# contextdb

**A temporal graph-vector database for AI systems that need memory.**

Most vector databases treat embeddings as the whole story. But AI systems that interact with the real world need facts that expire, sources that lie, memory that decays, and context that matters. contextdb handles all four.

[**Documentation**](https://antiartificial.github.io/contextdb) | [**Quick Start**](https://antiartificial.github.io/contextdb/quick-start) | [**Examples**](https://antiartificial.github.io/contextdb/examples) | [**API Reference**](https://antiartificial.github.io/contextdb/api/go-sdk)

## Five lines to a working database

```go
db := client.MustOpen(client.Options{})
defer db.Close()

ns := db.Namespace("my-app", namespace.ModeGeneral)
res, _ := ns.Write(ctx, client.WriteRequest{
    Content: "Go 1.22 added routing patterns to net/http",
    SourceID: "docs-crawler",
    Vector:   embedding,
})
results, _ := ns.Retrieve(ctx, client.RetrieveRequest{Vector: queryVec, TopK: 5})
```

Zero external dependencies. No Docker. No config files. One `go get` and you're running.

## What makes it different

| Feature | contextdb | Typical vector DB |
|:--------|:----------|:------------------|
| **Bi-temporal storage** | `valid_time` + `transaction_time` tracked independently | Single timestamp or none |
| **Source credibility** | Admission gate rejects trolls and spam at write time | Trust everything equally |
| **Memory decay** | Exponential decay with configurable half-lives per memory type | No decay model |
| **Hybrid retrieval** | Vector + graph + session fan-out with unified scoring | Vector-only |
| **Caller-supplied weights** | Similarity, confidence, recency, utility -- per query | Fixed ranking |
| **Namespace modes** | belief_system, agent_memory, general, procedural | One-size-fits-all |

## Scoring function

```
score = w_sim * cosine(candidate, query) + w_conf * confidence + w_rec * exp(-alpha * age) + w_util * utility
```

All weights normalised at query time. Different namespace modes ship tuned defaults.

## Deployment modes

| Mode | Backend | Use case |
|:-----|:--------|:---------|
| **Embedded** | In-memory or BadgerDB | Dev, testing, sidecars, CLIs |
| **Standard** | Postgres + pgvector | Production single-node |
| **Remote** | gRPC to contextdb server | Microservices |

## Quick start

```bash
go get github.com/antiartificial/contextdb@latest
```

```bash
# Run the server (no external dependencies)
make run

# With Postgres
docker compose up --build

# Run all tests
make test

# Coverage
make cover-text
```

## Project layout

```
contextdb/
├── cmd/contextdb/           # server entrypoint (gRPC + REST + observe)
├── internal/
│   ├── core/                # Node, Edge, Source, ScoreParams
│   ├── store/               # GraphStore, VectorIndex, KVStore, EventLog
│   │   ├── memory/          # in-process backend
│   │   ├── badger/          # BadgerDB + HNSW backend
│   │   └── postgres/        # Postgres + pgvector backend
│   ├── extract/             # LLM entity/relation extraction
│   ├── ingest/              # admission gate
│   ├── compact/             # RAPTOR hierarchical compaction
│   ├── retrieval/           # hybrid retrieval + scoring
│   ├── server/              # gRPC + REST + multi-tenancy
│   ├── namespace/           # mode presets
│   └── observe/             # metrics, pprof, health
├── pkg/client/              # Go SDK
├── bench/longmemeval/       # LongMemEval benchmark
├── deploy/helm/contextdb/   # Helm chart
└── docs/                    # Documentation (GitHub Pages)
```

## Related work

- [Zep / Graphiti](https://arxiv.org/abs/2501.13956) -- bi-temporal KG for agent memory
- [Hindsight](https://arxiv.org/abs/2512.12818) -- TEMPR multi-strategy retrieval
- [RAPTOR](https://arxiv.org/abs/2401.18059) -- hierarchical summarisation for compaction
- [A-MAC](https://arxiv.org/abs/2603.04549) -- adaptive memory admission control

## License

MIT
