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
  "admitted": true
}
```

### Retrieve

```
rpc Retrieve(GRPCRetrieveRequest) returns (GRPCRetrieveResponse)
```

```json
// Request
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

### Ping

```
rpc Ping(Empty) returns (PingResponse)
```

```json
// Response
{"Status": "ok"}
```

## Multi-tenancy

Set the `X-Tenant-ID` metadata header to isolate data by tenant:

```go
import "google.golang.org/grpc/metadata"

md := metadata.Pairs("x-tenant-id", "acme-corp")
ctx := metadata.NewOutgoingContext(ctx, md)
```

Or use a Bearer token where the token prefix is the tenant ID:

```
Authorization: Bearer acme-corp:secret-token
```

The tenant ID is prepended to the namespace: `acme-corp/my-app`.
