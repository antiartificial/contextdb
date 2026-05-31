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

Publish is dry-run-first so operators can validate the exact service entry before writing to Norn:

```bash
contextdb norn publish \
  --endpoint "$CONTEXTDB_URL" \
  --publish-url "$NORN_PUBLISH_URL" \
  --report
```

The dry-run report includes the generated entry and publish target without sending an HTTP request. When the authenticated Norn write endpoint is ready, publish explicitly:

```bash
contextdb norn publish \
  --endpoint "$CONTEXTDB_URL" \
  --publish-url "$NORN_PUBLISH_URL" \
  --token "$NORN_TOKEN" \
  --execute \
  --report
```

`contextdb norn publish` reads `NORN_PUBLISH_URL`, `NORN_PUBLISH_METHOD`, and `NORN_TOKEN` when the matching flags are omitted. It sends the validated manifest entry as JSON and sets `Authorization: Bearer ...` when a token is supplied.

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

The current non-breaking release is `v0.77.0`. Deduplication is opt-in via `WriteRequest.Dedup`, `Options.DedupWrites`, REST `dedup`, or `CONTEXTDB_DEDUP_WRITES=true`, so existing writers continue creating distinct nodes by default. Live services can report their package version and supported feature surface through `/v1/version`, `/v1/features`, `/v1/migrations`, and matching GraphQL introspection fields. Operators can also run `contextdb doctor --sample-write` when they want an explicit write/retrieve probe, `contextdb doctor --backup-marker PATH` when they want backup freshness in the report, `contextdb doctor --store-consistency --store-namespace NAME` when they want local store/index consistency evidence, `contextdb doctor --kv-key KEY` when they want local KV hot-key evidence, `contextdb doctor --kv-derived-key KEY --max-kv-derived-age 2h` when they want generated-at freshness evidence for derived KV values, `contextdb doctor --published-backup-url URL --max-published-backup-age 24h` when they want published backup catalog freshness in the combined health report, `contextdb doctor --published-backup-index PATH --published-backup-url URL` when they want local-vs-published backup catalog drift and a dry-run repair hint in the same report, `contextdb repair kv-cache --key KEY --value-file PATH --report` when they want a dry-run KV refresh plan, `contextdb repair kv-cache --key KEY --derive recent-nodes --derive-namespace NAME --report` when they want reviewed graph-derived session context refresh values, `contextdb repair kv-cache --key KEY --value-file PATH --execute --report` when they want to write reviewed cache refreshes, `contextdb repair vector-index --namespace NAME --report` when they want a dry-run repair plan for vector rebuild candidates, `contextdb repair vector-index --namespace NAME --execute --report` when they want to reindex reviewed candidates, `contextdb snapshot export --manifest PATH` when they want checksummed backup sidecars, `contextdb snapshot verify --manifest PATH --in PATH` when they want pre-restore artifact verification, `contextdb snapshot rehearse --manifest PATH --in PATH --namespace NAME` when they want a combined verify and dry-run restore preflight, `contextdb snapshot import --promotion-report PATH` when they want a promotion receipt, `contextdb snapshot receipt verify --promotion-report PATH --manifest PATH` when they want to verify promotion receipts, `contextdb snapshot lifecycle verify --summary PATH` when they want to verify a lifecycle summary and its referenced artifacts, `contextdb snapshot lifecycle retention --dir PATH --keep N` when they want a dry-run retention report for backup bundles, `contextdb snapshot lifecycle retention --emit-delete-script` when they want a reviewable deletion plan, `contextdb snapshot lifecycle index --dir PATH` when they want a compact backup manifest index, `contextdb snapshot lifecycle index verify --in PATH` when they want to verify an index, `contextdb snapshot lifecycle index diff --old PATH --new PATH` when they want to compare backup catalogs, `contextdb snapshot lifecycle index publish --in PATH` when they want to publish backup catalog metadata, `contextdb snapshot lifecycle index publish --execute --receipt-out PATH` when they want durable evidence for a catalog replacement, `contextdb snapshot lifecycle index publish drift --in PATH` when they want to compare local and published backup catalog metadata with a dry-run publish command hint, `contextdb snapshot lifecycle index publish freshness --published-url URL --max-age 24h` when they want to check published backup catalog freshness, `contextdb eval ranking` when they want a representative corpus score-drift snapshot, `contextdb eval ranking --markdown` when they want a release-review recap, `contextdb eval ranking --compare previous.json --diff-markdown` when they want release-to-release rank and score movement, `contextdb eval ranking --baseline-dir DIR` when they want versioned release baselines, `contextdb eval ranking --compare-baseline-dir DIR --diff-markdown` when they want the latest previous baseline resolved automatically, `contextdb eval ranking --baseline-retention-dir DIR --baseline-retention-keep N` when they want a read-only retained and pruneable baseline report, `contextdb eval ranking --baseline-retention-dir DIR --emit-delete-script` when they want a reviewable deletion plan for pruneable ranking baselines, `contextdb eval ranking --baseline-retention-dir DIR --baseline-manifest-out PATH` when they want bytes and hashes for CI baseline artifacts, `contextdb norn manifest` when they want to register or validate the Norn service entry, `contextdb norn drift` when they want to detect live manifest drift, and `contextdb norn publish` when they want a dry-run-first registration write. Feedback audits, source trust timelines with anomaly review tasks, claim review queues with durable decisions and reviewer filters, evidence-rich explain-rank APIs, acquisition plans, review handoff retry execution, backoff recommendations, endpoint-level retry fatigue summaries with owner and escalation Markdown handoff export, wider candidate-pool ranking, the scheduled backup runbook, and release health docs are available through the Go SDK, REST, GraphQL, and GitHub Pages.
