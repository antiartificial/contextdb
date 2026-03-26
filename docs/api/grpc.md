---
title: gRPC API
parent: API Reference
nav_order: 2
---

# gRPC API

contextdb exposes a gRPC service on port **7700** using JSON encoding (no protobuf codegen required).

## Connection

```go
import (
    "encoding/json"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

conn, err := grpc.Dial("localhost:7700",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithDefaultCallOptions(
        grpc.ForceCodec(JSONCodec{}),
    ),
)
```

The JSON codec:

```go
type JSONCodec struct{}
func (JSONCodec) Name() string                         { return "json" }
func (JSONCodec) Marshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
func (JSONCodec) Unmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }
```

## Authentication

Set the `authorization` metadata key with a Bearer token:

```go
import "google.golang.org/grpc/metadata"

md := metadata.Pairs("authorization", "Bearer acme-corp:write:sk-secret")
ctx := metadata.NewOutgoingContext(ctx, md)
```

See [RBAC](../concepts/rbac) for details on the token format and permission model.

## Service: `contextdb.v1.ContextDB`

### Write

```
rpc Write(GRPCWriteRequest) returns (GRPCWriteResponse)
```

```json
// Request
{
  "namespace": "my-app",
  "namespace_mode": "general",
  "content": "Go 1.22 added routing patterns",
  "source_id": "docs-crawler",
  "labels": ["Claim"],
  "vector": [0.1, 0.2, 0.3],
  "confidence": 0.9
}

// Response
{
  "node_id": "550e8400-e29b-41d4-a716-446655440000",
  "admitted": true,
  "conflict_ids": []
}
```

### Retrieve

```
rpc Retrieve(GRPCRetrieveRequest) returns (GRPCRetrieveResponse)
```

```json
// Request — vector-based
{
  "namespace": "my-app",
  "vector": [0.1, 0.2, 0.3],
  "top_k": 5,
  "score_params": {
    "similarity_weight": 0.5,
    "confidence_weight": 0.3,
    "recency_weight": 0.15,
    "utility_weight": 0.05
  }
}

// Request — text-based (auto-embedded server-side)
{
  "namespace": "my-app",
  "text": "What changed in Go 1.22?",
  "top_k": 5
}

// Request — with label filtering
{
  "namespace": "my-app",
  "text": "routing patterns",
  "labels": ["Claim"],
  "top_k": 10
}

// Response
{
  "results": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "namespace": "my-app",
      "labels": ["Claim"],
      "properties": {"text": "Go 1.22 added routing patterns"},
      "score": 0.87,
      "similarity_score": 0.95,
      "confidence_score": 0.9,
      "recency_score": 0.72,
      "utility_score": 0.5,
      "retrieval_source": "vector"
    }
  ]
}
```

### StreamRetrieve

Server-streamed variant of Retrieve. Results are sent one at a time as they are scored.

```
rpc StreamRetrieve(GRPCRetrieveRequest) returns (stream GRPCRetrieveResponse)
```

The request format is identical to `Retrieve`. Each streamed response contains a single result.

### IngestText

```
rpc IngestText(GRPCIngestRequest) returns (GRPCIngestResponse)
```

```json
// Request
{
  "namespace": "my-app",
  "namespace_mode": "general",
  "text": "Alice knows Go and Python. Bob specializes in Rust.",
  "source_id": "docs-crawler"
}

// Response
{
  "nodes_written": 4,
  "edges_written": 3,
  "rejected": 0
}
```

### LabelSource

```
rpc LabelSource(GRPCLabelSourceRequest) returns (Empty)
```

```json
// Request
{
  "namespace": "my-app",
  "namespace_mode": "general",
  "external_id": "user:spammer",
  "labels": ["troll"]
}
```

### GetNode

```
rpc GetNode(GRPCNodeRequest) returns (GRPCNodeResponse)
```

```json
// Request
{
  "namespace": "my-app",
  "node_id": "550e8400-e29b-41d4-a716-446655440000"
}

// Response — full node object
```

### WalkGraph

```
rpc WalkGraph(GRPCWalkRequest) returns (GRPCWalkResponse)
```

```json
// Request
{
  "namespace": "my-app",
  "seed_ids": ["550e8400-e29b-41d4-a716-446655440000"],
  "max_depth": 3
}
```

### Edges

```
rpc Edges(GRPCEdgesRequest) returns (GRPCEdgesResponse)
```

Returns edges connected to a node. Includes `contradicts` and `derived_from` edges.

### NodeHistory

```
rpc NodeHistory(GRPCNodeHistoryRequest) returns (GRPCNodeHistoryResponse)
```

Returns all versions of a node, ordered by transaction time.

### ManageSource

```
rpc ManageSource(GRPCManageSourceRequest) returns (Empty)
```

Admin-level source management operations.

### VectorOps

```
rpc VectorIndex(GRPCVectorIndexRequest) returns (Empty)
rpc VectorSearch(GRPCVectorSearchRequest) returns (GRPCVectorSearchResponse)
```

Direct vector index and search operations. Used by the remote store client.

### KV

```
rpc KV(GRPCKVRequest) returns (GRPCKVResponse)
```

Direct key-value operations. Used by the remote store client.

### Events

```
rpc EventAppend(GRPCEventAppendRequest) returns (Empty)
rpc EventSince(GRPCEventSinceRequest) returns (GRPCEventSinceResponse)
rpc EventMarkProcessed(GRPCEventMarkProcessedRequest) returns (Empty)
```

Direct event log operations. Used by the remote store client.

### Ping

```
rpc Ping(Empty) returns (PingResponse)
```

```json
// Response
{"Status": "ok"}
```

## Multi-tenancy

Set the `x-tenant-id` metadata header to isolate data by tenant:

```go
import "google.golang.org/grpc/metadata"

md := metadata.Pairs("x-tenant-id", "acme-corp")
ctx := metadata.NewOutgoingContext(ctx, md)
```

Or use a Bearer token where the token prefix is the tenant ID:

```
Authorization: Bearer acme-corp:read,write:secret-token
```

The tenant ID is prepended to the namespace: `acme-corp/my-app`.
