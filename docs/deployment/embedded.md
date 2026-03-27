---
title: Embedded Mode
parent: Deployment
nav_order: 1
---

# Embedded Mode

The fastest way to run contextdb. No Docker, no Postgres, no network. Just `go get` and write code.

## In-memory (zero persistence)

```go
db := client.MustOpen(client.Options{})
defer db.Close()
```

Data lives in process memory and is lost when the process exits. Ideal for:
- Unit tests
- Development
- Ephemeral workloads

## Persistent (BadgerDB)

```go
db, err := client.Open(client.Options{
    DataDir: "/var/lib/contextdb",
})
```

Data is persisted to disk using BadgerDB. The HNSW vector index is rebuilt from stored vectors on startup.

### What's stored where

| Component | Storage | Notes |
|:----------|:--------|:------|
| Nodes, edges, sources | BadgerDB LSM tree | Key-prefixed, namespace-isolated |
| Vector index | HNSW in-memory, backed by BadgerDB | Rebuilt on load |
| KV cache | BadgerDB with TTL | Native TTL support |
| Event log | BadgerDB with time-ordered keys | Append-only |

### Key schema

```
n/<namespace>/<nodeID>/<version>         → Node JSON
n-latest/<namespace>/<nodeID>            → Node JSON (latest version)
e/<namespace>/<edgeID>                   → Edge JSON
ei-src/<namespace>/<srcNodeID>/<edgeID>  → empty (index)
ei-dst/<namespace>/<dstNodeID>/<edgeID>  → empty (index)
s/<namespace>/<externalID>               → Source JSON
kv/<key>                                 → value + expiry
ev/<namespace>/<txTimeNano>/<eventID>    → Event JSON
vec/<namespace>/<entryID>                → VectorEntry JSON
```

## As a sidecar

contextdb compiles to a single static binary with no CGO dependency. Run it alongside your application:

```bash
# Build
CGO_ENABLED=0 go build -o contextdb ./cmd/contextdb

# Run with persistent storage
CONTEXTDB_DATA_DIR=/data ./contextdb
```

Ports:
- `:7700`: gRPC API
- `:7701`: REST API
- `:7702`: Metrics + pprof + health

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|:---------|:--------|:------------|
| `CONTEXTDB_MODE` | `embedded` | Storage mode |
| `CONTEXTDB_DATA_DIR` | (empty) | BadgerDB directory (empty = in-memory) |
| `CONTEXTDB_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `CONTEXTDB_GRPC_ADDR` | `:7700` | gRPC listen address |
| `CONTEXTDB_REST_ADDR` | `:7701` | REST listen address |
| `CONTEXTDB_OBS_ADDR` | `:7702` | Observe listen address |
