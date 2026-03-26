---
title: REST API
parent: API Reference
nav_order: 3
---

# REST API

contextdb exposes a REST API on port **7701**.

## Endpoints

| Method | Path | Description |
|:-------|:-----|:------------|
| `POST` | `/v1/namespaces/{ns}/write` | Write a node |
| `POST` | `/v1/namespaces/{ns}/retrieve` | Retrieve nodes |
| `POST` | `/v1/namespaces/{ns}/ingest` | Ingest text (LLM extraction) |
| `GET` | `/v1/namespaces/{ns}/nodes/{id}` | Get a single node |
| `POST` | `/v1/namespaces/{ns}/sources/label` | Label a source |
| `GET` | `/v1/stats` | Runtime statistics |
| `GET` | `/v1/ping` | Health check |

## Write

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/write \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "general",
    "content": "Go 1.22 added routing patterns to net/http",
    "source_id": "docs-crawler",
    "labels": ["Claim"],
    "vector": [0.1, 0.2, 0.3],
    "confidence": 0.9
  }'
```

**Response:**
```json
{
  "node_id": "550e8400-e29b-41d4-a716-446655440000",
  "admitted": true
}
```

**Rejected write:**
```json
{
  "node_id": "00000000-0000-0000-0000-000000000000",
  "admitted": false,
  "reason": "source credibility below floor (< 0.05)"
}
```

### Request fields

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `mode` | string | No | Namespace mode: `general`, `belief_system`, `agent_memory`, `procedural` |
| `content` | string | Yes | Text content |
| `source_id` | string | Yes | External source identifier |
| `labels` | string[] | No | Node labels |
| `properties` | object | No | Arbitrary metadata |
| `vector` | float[] | No | Pre-computed embedding |
| `model_id` | string | No | Embedding model identifier |
| `confidence` | float | No | Confidence [0, 1] |
| `valid_from` | string | No | ISO 8601 timestamp |
| `mem_type` | string | No | Memory type: `episodic`, `semantic`, `procedural`, `working` |

## Retrieve

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/retrieve \
  -H "Content-Type: application/json" \
  -d '{
    "vector": [0.1, 0.2, 0.3],
    "top_k": 5,
    "score_params": {
      "similarity_weight": 0.5,
      "confidence_weight": 0.3,
      "recency_weight": 0.15,
      "utility_weight": 0.05
    }
  }'
```

**Response:**
```json
{
  "results": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "namespace": "my-app",
      "labels": ["Claim"],
      "properties": {"text": "Go 1.22 added routing patterns to net/http"},
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

### Request fields

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `vector` | float[] | For vector search | Query embedding |
| `seed_ids` | string[] | For graph walk | Known relevant node IDs |
| `top_k` | int | No | Max results (default: 10) |
| `score_params` | object | No | Override scoring weights |
| `as_of` | string | No | ISO 8601 timestamp for point-in-time query |

## Ingest Text

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "general",
    "text": "Alice knows Go and Python. Bob specializes in Rust.",
    "source_id": "docs-crawler"
  }'
```

**Response:**
```json
{
  "nodes_written": 4,
  "edges_written": 3,
  "rejected": 0
}
```

## Get Node

```bash
curl http://localhost:7701/v1/namespaces/my-app/nodes/550e8400-e29b-41d4-a716-446655440000
```

## Label Source

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/sources/label \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "belief_system",
    "external_id": "moderator:alice",
    "labels": ["moderator"]
  }'
```

**Response:**
```json
{"status": "ok"}
```

## Stats

```bash
curl http://localhost:7701/v1/stats
```

**Response:**
```json
{
  "Mode": "embedded",
  "RetrievalTotal": 142,
  "RetrievalErrors": 0,
  "IngestTotal": 500,
  "IngestAdmitted": 487,
  "IngestRejected": 13,
  "LatencyP50Us": 450.5,
  "LatencyP95Us": 1200.3,
  "LatencyMeanUs": 520.1
}
```

## Health Check

```bash
curl http://localhost:7701/v1/ping
```

**Response:**
```json
{"status": "ok"}
```

## Multi-tenancy

Pass `X-Tenant-ID` header to isolate data:

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/write \
  -H "X-Tenant-ID: acme-corp" \
  -H "Content-Type: application/json" \
  -d '{"content": "...", "source_id": "..."}'
```

Or use Bearer token prefix:

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/write \
  -H "Authorization: Bearer acme-corp:secret-token" \
  -H "Content-Type: application/json" \
  -d '{"content": "...", "source_id": "..."}'
```

## Observability

Metrics and health are on port **7702**:

```bash
# Prometheus metrics
curl http://localhost:7702/metrics

# pprof
curl http://localhost:7702/debug/pprof/

# Health
curl http://localhost:7702/healthz
```
