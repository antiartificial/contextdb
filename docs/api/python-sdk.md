---
title: Python SDK
---

# Python SDK

The Python SDK is a REST client for contextdb, located in `sdk/python/`.

## Installation

```bash
pip install contextdb
```

## Connection

```python
from contextdb import ContextDB

db = ContextDB("http://localhost:7701")
```

The client accepts an optional `timeout` parameter (default: 30 seconds):

```python
db = ContextDB("http://localhost:7701", timeout=60.0)
```

### Context manager

```python
with ContextDB("http://localhost:7701") as db:
    ns = db.namespace("my-app")
    # ...
# client is closed automatically
```

## Namespace

```python
ns = db.namespace("my-app", mode="general")
```

The `mode` parameter accepts: `general`, `belief_system`, `agent_memory`, `procedural`.

## Write

```python
result = ns.write(
    content="Go 1.22 added routing patterns to net/http",
    source_id="docs-crawler",
    labels=["Claim"],
    properties={"url": "https://go.dev/blog"},
    confidence=0.9,
)

print(result.node_id)    # UUID string
print(result.admitted)   # True/False
print(result.reason)     # rejection reason if not admitted
```

### WriteResult fields

| Field | Type | Description |
|:------|:-----|:------------|
| `node_id` | `str` | UUID of the written or conflicting node |
| `admitted` | `bool` | Whether the write was accepted |
| `reason` | `str` | Rejection reason (empty if admitted) |
| `conflict_ids` | `list[str]` | UUIDs of contradicting nodes |

## Retrieve

```python
# Vector-based query
results = ns.retrieve(vector=[0.1, 0.2, 0.3], top_k=5)

# Text-based query (auto-embedded server-side)
results = ns.retrieve(text="What changed in Go 1.22?", top_k=5)

# With label filtering
results = ns.retrieve(
    text="routing patterns",
    labels=["Claim"],
    top_k=10,
)

# With custom score weights
results = ns.retrieve(
    text="routing patterns",
    top_k=5,
    score_params={
        "similarity_weight": 0.6,
        "confidence_weight": 0.2,
        "recency_weight": 0.1,
        "utility_weight": 0.1,
    },
)

for r in results:
    print(f"{r.score:.2f} {r.node.properties['text']}")
```

### Result fields

| Field | Type | Description |
|:------|:-----|:------------|
| `node` | `Node` | Full node with labels, properties, metadata |
| `score` | `float` | Composite score [0, 1] |
| `similarity_score` | `float` | Vector similarity component |
| `confidence_score` | `float` | Confidence component |
| `recency_score` | `float` | Recency component |
| `utility_score` | `float` | Utility component |
| `retrieval_source` | `str` | "vector", "graph", or "fused" |

## Ingest text

```python
result = ns.ingest_text(
    text="Alice knows Go and Python. Bob specializes in Rust.",
    source_id="docs-crawler",
)

print(result.nodes_written)   # 4
print(result.edges_written)   # 3
print(result.rejected)        # 0
```

## Label source

```python
ns.label_source("user:spammer", labels=["troll"])
ns.label_source("moderator:alice", labels=["moderator"])
```

## Health check

```python
db.ping()   # {"status": "ok"}
db.stats()  # server statistics dict
```

## Authentication

Pass a Bearer token via the `Authorization` header by subclassing or configuring the underlying `httpx` client:

```python
import httpx

client = httpx.Client(
    base_url="http://localhost:7701",
    headers={"Authorization": "Bearer acme-corp:write:sk-secret"},
)
db = ContextDB.__new__(ContextDB)
db._client = client
```
