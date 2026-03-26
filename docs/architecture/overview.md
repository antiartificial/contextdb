---
title: System Overview
parent: Architecture
nav_order: 1
---

# System Overview

contextdb is a layered system with pluggable storage backends and parallel retrieval paths.

## Component diagram

```mermaid
graph TB
    subgraph Clients
        SDK[Go SDK<br/>pkg/client]
        GRPC[gRPC Client<br/>:7700]
        REST[REST Client<br/>:7701]
    end

    subgraph Server Layer
        GS[gRPC Server]
        RS[REST Server]
        TM[Tenant Middleware]
    end

    subgraph Core
        NS[Namespace Manager]
        WP[Write Path<br/>Admission Gate]
        RP[Read Path<br/>Hybrid Retrieval]
        EX[LLM Extractor]
        CO[RAPTOR Compactor]
    end

    subgraph Store Interfaces
        GI[GraphStore]
        VI[VectorIndex]
        KV[KVStore]
        EL[EventLog]
    end

    subgraph Backends
        MEM[Memory<br/>In-process]
        BAD[BadgerDB + HNSW<br/>Embedded]
        PG[Postgres + pgvector<br/>Standard]
    end

    subgraph Observe
        MET[Metrics :7702]
        PP[pprof]
        HL[Health Check]
    end

    SDK --> NS
    GRPC --> GS --> TM --> NS
    REST --> RS --> TM --> NS
    NS --> WP
    NS --> RP
    WP --> EX
    WP --> GI & VI & EL
    RP --> GI & VI & KV
    CO --> GI & VI & EL
    GI & VI & KV & EL --> MEM
    GI & VI & KV & EL --> BAD
    GI & VI & KV & EL --> PG

    style SDK fill:#4a9eff,stroke:#333,color:#fff
    style MEM fill:#2ecc71,stroke:#333,color:#fff
    style BAD fill:#27ae60,stroke:#333,color:#fff
    style PG fill:#16a085,stroke:#333,color:#fff
```

## Layer responsibilities

### Client layer (`pkg/client`)
- `DB` -- connection handle, analogous to `sql.DB`
- `NamespaceHandle` -- scoped read/write operations
- Three modes: embedded (in-process), standard (Postgres), remote (gRPC)

### Server layer (`internal/server`)
- gRPC server on `:7700` with JSON codec (no protobuf codegen required)
- REST server on `:7701` with Go 1.22+ routing patterns
- Multi-tenant isolation via `X-Tenant-ID` header or Bearer token prefix
- Observe server on `:7702` with Prometheus metrics, pprof, and health check

### Write path (`internal/ingest`)
- Source resolution and credibility lookup
- Admission gate: credibility floor, near-duplicate detection, novelty threshold
- Graph upsert + vector indexing + event logging

### Read path (`internal/retrieval`)
- Concurrent fan-out: vector search + graph traversal + session context
- Fusion: deduplicate and merge results from all paths
- Scoring: composite score with caller-supplied weights

### Store interfaces (`internal/store`)
- `GraphStore` -- node/edge CRUD, versioning, walk
- `VectorIndex` -- ANN search, index, delete
- `KVStore` -- key-value with TTL (caching, sessions)
- `EventLog` -- append-only temporal event stream

### Backends
- **Memory** -- in-process maps and slices, zero dependencies
- **BadgerDB + HNSW** -- embedded persistent storage, single binary
- **Postgres + pgvector** -- production-grade with recursive CTE graph traversal

## Project layout

```
contextdb/
├── cmd/contextdb/           # server entrypoint
├── internal/
│   ├── core/                # domain types: Node, Edge, Source, ScoreParams
│   ├── store/               # store interfaces
│   │   ├── memory/          # in-process backend
│   │   ├── badger/          # BadgerDB + HNSW backend
│   │   └── postgres/        # Postgres + pgvector backend
│   ├── extract/             # LLM entity/relation extraction
│   ├── ingest/              # write path: admission gate
│   ├── compact/             # RAPTOR hierarchical compaction
│   ├── retrieval/           # read path: fusion, scoring
│   ├── server/              # gRPC + REST servers
│   ├── namespace/           # mode presets and config
│   └── observe/             # metrics, pprof, health
├── pkg/client/              # Go SDK
├── bench/                   # benchmarks and evaluation
│   └── longmemeval/         # LongMemEval benchmark harness
└── deploy/helm/contextdb/   # Helm chart for Kubernetes
```
