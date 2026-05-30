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

## Registration Helper

Generate the expected Norn service entry from the same defaults used by the server:

```bash
contextdb norn manifest \
  --endpoint "https://your-contextdb-route.example" \
  --name contextdb-mini
```

The command prints a JSON entry with the `contextdb` app id, current contextdb version, REST endpoint, health URL, GraphQL URL, feature metadata URL, default ports, and tags. It reads these environment variables when flags are omitted:

| Environment variable | Used for |
|:---------------------|:---------|
| `CONTEXTDB_PUBLIC_URL` | Public REST endpoint advertised by Norn |
| `CONTEXTDB_GRPC_ADDR` | gRPC listen address, default `:7700` |
| `CONTEXTDB_REST_ADDR` | REST listen address, default `:7701` |
| `CONTEXTDB_OBS_ADDR` | observe listen address, default `:7702` |

Validate an entry before registering it:

```bash
contextdb norn manifest --endpoint "$CONTEXTDB_URL" > contextdb.norn.json
contextdb norn validate --file contextdb.norn.json
```

Validation checks that the entry is for `app: contextdb`, has an absolute endpoint URL, includes a service name, and advertises a REST port.

Compare the expected local entry with the live Norn manifest:

```bash
contextdb norn drift --manifest-url "$NORN_MANIFEST_URL" --endpoint "$CONTEXTDB_URL"
```

The drift report returns `ok: true` when the generated entry matches the live manifest. When fields differ, it prints `diffs` entries such as `endpoint`, `version`, `ports.rest`, or `tags` and exits non-zero.

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

The current non-breaking release is `v0.26.0`. Deduplication is opt-in via `WriteRequest.Dedup`, `Options.DedupWrites`, REST `dedup`, or `CONTEXTDB_DEDUP_WRITES=true`, so existing writers continue creating distinct nodes by default. Live services can report their package version and supported feature surface through `/v1/version`, `/v1/features`, `/v1/migrations`, and matching GraphQL introspection fields. Operators can also run `contextdb doctor --sample-write` when they want an explicit write/retrieve probe, `contextdb doctor --backup-marker PATH` when they want backup freshness in the report, `contextdb snapshot export --manifest PATH` when they want checksummed backup sidecars, `contextdb snapshot verify --manifest PATH --in PATH` when they want pre-restore artifact verification, `contextdb snapshot rehearse --manifest PATH --in PATH --namespace NAME` when they want a combined verify and dry-run restore preflight, `contextdb snapshot import --promotion-report PATH` when they want a promotion receipt, `contextdb norn manifest` when they want to register or validate the Norn service entry, and `contextdb norn drift` when they want to detect live manifest drift. Feedback audits, source trust timelines with anomaly review tasks, claim review queues with durable decisions and reviewer filters, evidence-rich explain-rank APIs, acquisition plans, wider candidate-pool ranking, the scheduled backup runbook, and release health docs are available through the Go SDK, REST, GraphQL, and GitHub Pages.
