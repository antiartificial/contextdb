---
title: Scaled Deployment
---

# Scaled Deployment

`ModeScaled` distributes storage across dedicated backends: **Qdrant** for vectors, **Redis** for key-value and event log, and **Postgres** for the graph store. This enables higher throughput and independent scaling of each component.

## Architecture

```mermaid
graph TB
    subgraph Application
        APP[Your App]
        SDK[Go SDK / Python / TypeScript]
    end

    subgraph contextdb Server
        GRPC[gRPC :7700]
        REST[REST :7701]
        OBS[Observe :7702<br/>Admin UI + Metrics]
    end

    subgraph Vector Store
        QD[Qdrant :6334<br/>ANN search + label filtering]
        QV[(Qdrant Volume)]
    end

    subgraph Key-Value + Events
        RD[Redis :6379<br/>KV + EventLog]
    end

    subgraph Graph Store
        PG[Postgres :5432<br/>pgvector + graph CTE]
        PV[(Postgres Volume)]
    end

    APP --> SDK
    SDK --> GRPC & REST
    GRPC & REST --> QD
    GRPC & REST --> RD
    GRPC & REST --> PG
    QD --> QV
    PG --> PV

    style QD fill:#8e44ad,stroke:#333,color:#fff
    style RD fill:#c0392b,stroke:#333,color:#fff
    style PG fill:#16a085,stroke:#333,color:#fff
```

## Configuration

### Go SDK

```go
db, err := client.Open(client.Options{
    Mode:             client.ModeScaled,
    DSN:              "postgres://user:pass@localhost:5432/contextdb?sslmode=disable",
    QdrantAddr:       "localhost:6334",
    RedisAddr:        "localhost:6379",
    VectorDimensions: 1536,
})
```

| Option | Type | Description |
|:-------|:-----|:------------|
| `Mode` | `Mode` | Must be `ModeScaled` |
| `DSN` | `string` | Postgres connection string for graph store |
| `QdrantAddr` | `string` | Qdrant gRPC address |
| `RedisAddr` | `string` | Redis address |
| `VectorDimensions` | `int` | Embedding dimensionality (default: 1536) |

### Docker Compose

The `docker-compose.yml` includes a `scaled` profile with Qdrant and Redis:

```bash
docker compose --profile scaled up --build
```

This starts:

| Service | Port | Purpose |
|:--------|:-----|:--------|
| `contextdb` | 7700, 7701 | gRPC + REST server |
| `postgres` | 5432 | Graph store with pgvector |
| `qdrant` | 6334 | Vector index |
| `redis` | 6379 | KV store + event log |

All services have persistent volumes and health checks.

## Backend details

### Qdrant

The Qdrant backend implements `store.VectorIndex`:
- Collections are named `contextdb_{namespace}` with cosine distance
- Label filtering is pushed down to Qdrant as metadata filters
- Vectors are stored with node metadata (ID, namespace, labels)
- Requires the `integration` build tag for compilation

### Redis

The Redis backend implements `store.KVStore` and `store.EventLog`:
- Keys are prefixed with `contextdb:kv:` by default
- TTL is supported via `SET ... EX` for session data and caches
- Event log entries are stored in Redis sorted sets

### Postgres

Postgres continues to serve as the graph store in scaled mode, providing:
- Node and edge CRUD with versioning
- Recursive CTE graph traversal for `Walk` operations
- Source credibility tracking

## When to use scaled mode

| Scenario | Recommended mode |
|:---------|:-----------------|
| Development and testing | Embedded (in-memory) |
| Single-node production | Standard (Postgres only) |
| Multi-service architecture | Remote (gRPC client) |
| High vector throughput, large datasets | **Scaled** |
| Independent scaling of vector/graph/cache | **Scaled** |
