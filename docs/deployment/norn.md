---
title: Mini/Norn Deployment
---

# Mini/Norn Deployment

The internal live contextdb instance runs on Aaron's Mac mini and is managed by Norn. Norn is the discovery and routing layer; the service manifest should be treated as the source of truth for the current HTTP endpoint.

## Discovery

```bash
export NORN_MANIFEST_URL="https://aarons-mac-mini.tail113139.ts.net/api/services/manifest"

curl -fsS "$NORN_MANIFEST_URL" \
  | jq '.services[] | select(.app == "contextdb")'
```

When contextdb is registered, the matching manifest entry should expose an endpoint URL. Use that URL as `CONTEXTDB_URL`.

```bash
export CONTEXTDB_URL="https://your-contextdb-route.example"
```

If the query returns no service, contextdb may be running but not advertised. Check the Norn registration on the mini before changing client configuration.

## Health Checks

REST health:

```bash
curl "$CONTEXTDB_URL/v1/ping"
```

GraphQL health:

```bash
curl -X POST "$CONTEXTDB_URL/graphql" \
  -H "Content-Type: application/json" \
  -d '{"query":"{ search(namespace:\"my-app\", query:\"status\") { totalCount } }"}'
```

Observe endpoints, when exposed, follow the server defaults:

| Port | Purpose |
|:-----|:--------|
| `7700` | gRPC API |
| `7701` | REST and GraphQL |
| `7702` | Metrics, pprof, health, admin surface |

## Client Configuration

REST clients should use the Norn HTTP endpoint:

```python
from contextdb import ContextDB

db = ContextDB("https://your-contextdb-route.example")
```

Go clients can either connect over remote gRPC when that endpoint is exposed, or use REST through the language SDKs and tools.

```go
db := client.MustOpen(client.Options{
    Mode: client.ModeRemote,
    Addr: "your-contextdb-grpc-route:7700",
})
```

## Release Notes

The current non-breaking release is `v0.11.2`. Deduplication is opt-in via `WriteRequest.Dedup`, `Options.DedupWrites`, REST `dedup`, or `CONTEXTDB_DEDUP_WRITES=true`, so existing writers continue creating distinct nodes by default. Live services can report their package version and supported feature surface through `/v1/version`, `/v1/features`, `/v1/migrations`, and matching GraphQL introspection fields. Operators can also run `contextdb doctor --sample-write` when they want an explicit write/retrieve probe, and `contextdb doctor --backup-marker PATH` when they want backup freshness in the report. Feedback audits, source trust timelines, claim review queues, evidence-rich explain-rank APIs, acquisition plans, wider candidate-pool ranking, and release health docs are available through the Go SDK, REST, GraphQL, and GitHub Pages.
