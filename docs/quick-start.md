---
title: Quick Start
nav_order: 2
---

# Quick Start

Get contextdb running in under a minute.

## Install

```bash
go get github.com/antiartificial/contextdb@latest
```

## Embedded mode (zero dependencies)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/antiartificial/contextdb/internal/namespace"
    "github.com/antiartificial/contextdb/pkg/client"
)

func main() {
    ctx := context.Background()

    // Open an in-memory database -- no config, no Docker, no network
    db := client.MustOpen(client.Options{})
    defer db.Close()

    // Create a namespace with general-purpose scoring defaults
    ns := db.Namespace("my-app", namespace.ModeGeneral)

    // Write a fact (with a pre-computed embedding vector)
    embedding := make([]float32, 128) // replace with your real embedding
    embedding[0] = 0.42

    res, err := ns.Write(ctx, client.WriteRequest{
        Content:    "Go 1.22 added routing patterns to net/http",
        SourceID:   "docs-crawler",
        Labels:     []string{"Claim"},
        Vector:     embedding,
        Confidence: 0.9,
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Written: node=%s admitted=%v\n", res.NodeID, res.Admitted)

    // Retrieve by vector similarity
    queryVec := make([]float32, 128)
    queryVec[0] = 0.40

    results, err := ns.Retrieve(ctx, client.RetrieveRequest{
        Vector: queryVec,
        TopK:   5,
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, r := range results {
        fmt.Printf("  score=%.3f  text=%s\n", r.Score, r.Node.Properties["text"])
    }
}
```

## Persistent embedded mode (BadgerDB)

Add a `DataDir` to persist data across restarts:

```go
db, err := client.Open(client.Options{
    DataDir: "/tmp/contextdb-data",
})
```

The database uses BadgerDB for key-value storage and an in-process HNSW index for vector search. No external services needed.

## Postgres mode

For production workloads, use Postgres with pgvector:

```go
db, err := client.Open(client.Options{
    Mode: client.ModeStandard,
    DSN:  "postgres://user:pass@localhost:5432/contextdb?sslmode=disable",
})
```

contextdb runs migrations automatically on first connection.

## Server mode

Run contextdb as a standalone server:

```bash
# Build and run
make run

# Or with Docker Compose (includes Postgres)
docker compose up --build
```

The server exposes three ports:

| Port | Protocol | Purpose |
|:-----|:---------|:--------|
| 7700 | gRPC | Primary API (JSON codec) |
| 7701 | HTTP | REST API |
| 7702 | HTTP | Metrics, pprof, health |

## What's next?

- [Concepts: Scoring](concepts/scoring) -- understand the retrieval scoring function
- [Concepts: Namespaces](concepts/namespaces) -- choose the right mode for your use case
- [Examples](examples) -- batteries-included recipes
- [API Reference: Go SDK](api/go-sdk) -- full SDK documentation
