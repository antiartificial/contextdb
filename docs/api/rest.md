---
title: REST API
---

# REST API

contextdb exposes a REST API on port **7701**.

## Endpoints

| Method | Path | Description |
|:-------|:-----|:------------|
| `POST` | `/v1/namespaces/{ns}/write` | Write a node |
| `POST` | `/v1/namespaces/{ns}/retrieve` | Retrieve nodes |
| `POST` | `/v1/namespaces/{ns}/rank/explain` | Explain ranking difference between two nodes |
| `POST` | `/v1/namespaces/{ns}/ingest` | Ingest text (LLM extraction) |
| `GET` | `/v1/namespaces/{ns}/nodes/{id}` | Get a single node |
| `POST` | `/v1/namespaces/{ns}/sources/label` | Label a source |
| `GET` | `/v1/namespaces/{ns}/sources/{sourceID}/trust` | Source credibility timeline |
| `GET` | `/v1/namespaces/{ns}/review/queue` | Claim review queue |
| `GET` | `/v1/namespaces/{ns}/review/decisions` | Review workflow decision history |
| `POST` | `/v1/namespaces/{ns}/review/decisions` | Record review assignment, snooze, or resolution |
| `POST` | `/v1/namespaces/{ns}/nodes/{id}/validate` | Validate a claim |
| `POST` | `/v1/namespaces/{ns}/nodes/{id}/refute` | Refute a claim |
| `POST` | `/v1/namespaces/{ns}/nodes/{id}/useful` | Mark a memory useful |
| `POST` | `/v1/namespaces/{ns}/nodes/{id}/stale` | Mark a node stale |
| `GET` | `/v1/namespaces/{ns}/feedback/events` | List feedback audit events |
| `GET` | `/v1/namespaces/{ns}/nodes/{id}/narrative` | Explain a claim with evidence |
| `POST` | `/v1/namespaces/{ns}/gaps` | Detect knowledge gaps |
| `POST` | `/v1/namespaces/{ns}/acquisition/plan` | Plan research, crawl, verification, and refresh tasks |
| `GET` | `/v1/stats` | Runtime statistics |
| `GET` | `/v1/ping` | Health check |
| `GET` | `/v1/version` | Release, API, feature, and migration summary |
| `GET` | `/v1/features` | Supported feature list |
| `GET` | `/v1/migrations` | Embedded Postgres migration list |

## Authentication

Pass a Bearer token in the `Authorization` header. The token format is `tenant:permissions:secret`:

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/write \
  -H "Authorization: Bearer acme-corp:write:sk-secret" \
  -H "Content-Type: application/json" \
  -d '{"content": "...", "source_id": "..."}'
```

See [RBAC](../concepts/rbac) for details on the token format and permission model.

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

**Write with conflict:**
```json
{
  "node_id": "550e8400-e29b-41d4-a716-446655440000",
  "admitted": true,
  "conflict_ids": ["660e8400-e29b-41d4-a716-446655440001"]
}
```

### Request fields

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `mode` | string | No | Namespace mode: `general`, `belief_system`, `agent_memory`, `procedural` |
| `content` | string | Yes | Text content (auto-embedded if no vector provided and server has embedder) |
| `source_id` | string | Yes | External source identifier |
| `labels` | string[] | No | Node labels |
| `properties` | object | No | Arbitrary metadata |
| `vector` | float[] | No | Pre-computed embedding |
| `model_id` | string | No | Embedding model identifier |
| `confidence` | float | No | Confidence [0, 1] |
| `valid_from` | string | No | ISO 8601 timestamp |
| `mem_type` | string | No | Memory type: `episodic`, `semantic`, `procedural`, `working` |
| `dedup` | bool | No | Opt this write into content fingerprint deduplication |
| `skip_dedup` | bool | No | Bypass content fingerprint deduplication when the server default is enabled |

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

### Text-based query

Send `text` instead of `vector` to have the server auto-embed the query:

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/retrieve \
  -H "Content-Type: application/json" \
  -d '{
    "text": "What changed in Go 1.22?",
    "top_k": 5
  }'
```

### Label filtering

Filter results to nodes with all specified labels:

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/retrieve \
  -H "Content-Type: application/json" \
  -d '{
    "text": "routing patterns",
    "labels": ["Claim", "Verified"],
    "top_k": 10
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
      "score_breakdown": {
        "similarity": 0.38,
        "confidence": 0.27,
        "recency": 0.14,
        "utility": 0.05
      },
      "retrieval_source": "vector"
    }
  ]
}
```

Raw component fields (`similarity_score`, `confidence_score`, `recency_score`, `utility_score`) stay in the response for debugging. `score_breakdown` reports the weighted contribution of each component; the four values sum to `score`.

### Request fields

| Field | Type | Required | Description |
|:------|:-----|:---------|:------------|
| `vector` | float[] | For vector search | Query embedding |
| `vectors` | float[][] | For multi-vector | Multiple query embeddings fused |
| `text` | string | For text search | Auto-embedded server-side |
| `seed_ids` | string[] | For graph walk | Known relevant node IDs |
| `top_k` | int | No | Max results (default: 10) |
| `labels` | string[] | No | Filter to nodes with all specified labels |
| `score_params` | object | No | Override scoring weights |
| `as_of` | string | No | ISO 8601 timestamp for point-in-time query |

## Introspection

Use the introspection endpoints to confirm what a live server supports:

```bash
curl http://localhost:7701/v1/version
```

```json
{
  "version": "0.38.0",
  "api_version": "v1",
  "docs_version": "0.38.0",
  "compatibility": "non-breaking pre-1.0 minor release",
  "latest_migration": 2,
  "features": [
    {
      "name": "feature-introspection",
      "status": "stable",
      "since": "v0.4.0",
      "description": "REST and GraphQL version, feature, and migration discovery endpoints."
    },
    {
      "name": "doctor-sample-write",
      "status": "stable",
      "since": "v0.4.1",
      "description": "Opt-in doctor write/retrieve probe for live REST deployments."
    },
    {
      "name": "feedback-event-log",
      "status": "stable",
      "since": "v0.5.0",
      "description": "Durable feedback audit events exposed through the Go SDK, REST, and GraphQL."
    },
    {
      "name": "source-trust-timeline",
      "status": "stable",
      "since": "v0.6.0",
      "description": "Source credibility timeline points derived from durable feedback events."
    },
    {
      "name": "claim-review-queue",
      "status": "stable",
      "since": "v0.7.0",
      "description": "Derived review tasks for refuted, stale, low-confidence, and contradictory claims."
    },
    {
      "name": "explain-rank",
      "status": "stable",
      "since": "v0.8.0",
      "description": "Compare two nodes and explain ranking differences with score component deltas."
    },
    {
      "name": "knowledge-acquisition-planner",
      "status": "stable",
      "since": "v0.9.0",
      "description": "Convert knowledge gaps and weak claims into prioritized source-backed acquisition tasks."
    },
    {
      "name": "doctor-backup-readiness",
      "status": "stable",
      "since": "v0.10.0",
      "description": "Opt-in doctor check for recent backup marker evidence."
    },
    {
      "name": "explain-rank-graph-evidence",
      "status": "stable",
      "since": "v0.11.0",
      "description": "Support-chain evidence and compound confidence in rank explanations."
    },
    {
      "name": "release-health-page",
      "status": "stable",
      "since": "v0.11.2",
      "description": "Release gate summary for unit, docs, ranking, durability, API contract, and race/soak checks."
    },
    {
      "name": "review-workflow-persistence",
      "status": "stable",
      "since": "v0.12.0",
      "description": "Append-only review decisions for assignment, status, resolution notes, and re-check scheduling."
    },
    {
      "name": "source-trust-anomaly-alerts",
      "status": "stable",
      "since": "v0.13.0",
      "description": "Review queue tasks for source credibility drops, low trust thresholds, and repeated refutations."
    },
    {
      "name": "norn-registration-helper",
      "status": "stable",
      "since": "v0.14.0",
      "description": "CLI helper to generate and validate contextdb Norn manifest entries."
    },
    {
      "name": "review-queue-filters",
      "status": "stable",
      "since": "v0.15.0",
      "description": "Review queue filters for task type, source, workflow status, and owner across Go SDK, REST, and GraphQL."
    },
    {
      "name": "norn-live-drift-check",
      "status": "stable",
      "since": "v0.16.0",
      "description": "CLI drift check that compares the expected contextdb Norn manifest entry with the live Norn manifest."
    },
    {
      "name": "snapshot-backup-restore",
      "status": "stable",
      "since": "v0.17.0",
      "description": "Public snapshot export/import helpers and CLI backup/restore commands with dry-run validation."
    },
    {
      "name": "snapshot-restore-report",
      "status": "stable",
      "since": "v0.18.0",
      "description": "Snapshot dry-run and import reports summarize processed lines, records, vectors, and namespace overrides."
    },
    {
      "name": "snapshot-backup-marker",
      "status": "stable",
      "since": "v0.19.0",
      "description": "Snapshot export can write a backup marker after a successful backup for doctor readiness checks."
    },
    {
      "name": "snapshot-diff-preview",
      "status": "stable",
      "since": "v0.20.0",
      "description": "Snapshot restore reports include new, changed, and unchanged node counts for previewing imports."
    },
    {
      "name": "backup-runbook",
      "status": "stable",
      "since": "v0.21.0",
      "description": "Documented backup workflow for scheduled snapshot export, restore preview, marker checks, and Norn pairing."
    },
    {
      "name": "backup-artifact-manifest",
      "status": "stable",
      "since": "v0.22.0",
      "description": "Snapshot export can write a checksummed JSON sidecar with backup metadata and record counts."
    },
    {
      "name": "backup-manifest-verify",
      "status": "stable",
      "since": "v0.23.0",
      "description": "Snapshot verify checks a backup file against its artifact manifest checksum, size, and record counts."
    },
    {
      "name": "restore-rehearsal",
      "status": "stable",
      "since": "v0.24.0",
      "description": "Snapshot rehearse verifies a backup artifact and runs a dry-run restore report in one preflight command."
    },
    {
      "name": "restore-promotion-checklist",
      "status": "stable",
      "since": "v0.25.0",
      "description": "Snapshot rehearsal reports include promotion metadata and a recommended import command."
    },
    {
      "name": "restore-promotion-receipt",
      "status": "stable",
      "since": "v0.26.0",
      "description": "Snapshot import can write a JSON promotion receipt with operator note and import counts."
    },
    {
      "name": "promotion-receipt-verify",
      "status": "stable",
      "since": "v0.27.0",
      "description": "Snapshot receipt verification compares promotion receipts against artifact manifests."
    },
    {
      "name": "backup-lifecycle-bundle",
      "status": "stable",
      "since": "v0.28.0",
      "description": "Backup runbook includes a guarded lifecycle script for export, verify, rehearse, optional promote, receipt verify, and summary output."
    },
    {
      "name": "lifecycle-summary-verify",
      "status": "stable",
      "since": "v0.29.0",
      "description": "Snapshot lifecycle verification checks a lifecycle summary and its referenced backup, manifest, rehearsal, promotion, and receipt-check artifacts."
    },
    {
      "name": "lifecycle-retention-report",
      "status": "stable",
      "since": "v0.30.0",
      "description": "Snapshot lifecycle retention reports group backup bundles and mark newest artifacts to keep versus older pruneable bundles without deleting files."
    },
    {
      "name": "lifecycle-delete-plan",
      "status": "stable",
      "since": "v0.31.0",
      "description": "Snapshot lifecycle retention can emit a reviewed shell deletion plan for pruneable artifacts without deleting files."
    },
    {
      "name": "lifecycle-manifest-index",
      "status": "stable",
      "since": "v0.32.0",
      "description": "Snapshot lifecycle index writes a compact JSON catalog of backup bundles, retention decisions, artifact sizes, and hashes."
    },
    {
      "name": "lifecycle-index-verify",
      "status": "stable",
      "since": "v0.33.0",
      "description": "Snapshot lifecycle index verification re-checks indexed artifact existence, sizes, and hashes."
    },
    {
      "name": "lifecycle-index-diff",
      "status": "stable",
      "since": "v0.34.0",
      "description": "Snapshot lifecycle index diff compares backup catalogs across runs or hosts for bundle and artifact changes."
    },
    {
      "name": "norn-manifest-publish",
      "status": "stable",
      "since": "v0.35.0",
      "description": "Norn manifest publish validates a dry-run plan by default and can explicitly publish the service entry to a configured Norn endpoint."
    },
    {
      "name": "lifecycle-index-publish",
      "status": "stable",
      "since": "v0.36.0",
      "description": "Snapshot lifecycle index publish validates and optionally sends backup catalog metadata to a configured ops endpoint without uploading backup contents."
    },
    {
      "name": "review-escalation-rules",
      "status": "stable",
      "since": "v0.37.0",
      "description": "Review queue escalation metadata flags aged assigned or snoozed items and high-priority source anomaly tasks."
    },
    {
      "name": "review-escalation-digest",
      "status": "stable",
      "since": "v0.38.0",
      "description": "Review escalation digests summarize escalated queue items by owner, source, item type, and escalation level."
    }
  ],
  "migrations": [
    { "version": 1, "name": "initial" },
    { "version": 2, "name": "node_fingerprints" }
  ],
  "recommended_docs": "/contextdb/",
  "release_notes_path": "/contextdb/releases/v0.38.0"
}
```

`/v1/features` returns only the feature list plus the server version. `/v1/migrations` returns the embedded migration list and latest migration version.

## Explain Rank

Compare two existing nodes under the namespace scoring model:

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/rank/explain \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "belief_system",
    "node_id": "550e8400-e29b-41d4-a716-446655440000",
    "other_node_id": "660e8400-e29b-41d4-a716-446655440001",
    "vector": [0.1, 0.2, 0.3]
  }'
```

**Response:**

```json
{
  "winner_node_id": "550e8400-e29b-41d4-a716-446655440000",
  "loser_node_id": "660e8400-e29b-41d4-a716-446655440001",
  "margin": 0.22,
  "summary": "550e8400-e29b-41d4-a716-446655440000 ranks above 660e8400-e29b-41d4-a716-446655440001 by 0.2200 points; confidence contributes the largest difference.",
  "node": {
    "node_id": "550e8400-e29b-41d4-a716-446655440000",
    "score": 0.82,
    "evidence": {
      "compound_confidence": 0.68,
      "support_count": 1,
      "links": [
        {
          "node_id": "770e8400-e29b-41d4-a716-446655440002",
          "edge_id": "880e8400-e29b-41d4-a716-446655440003",
          "edge_weight": 0.8,
          "confidence": 0.9,
          "text": "Runbook confirms blue-green deployment"
        }
      ]
    }
  },
  "factors": [
    {
      "factor": "confidence",
      "node_contribution": 0.43,
      "other_contribution": 0.09,
      "delta": 0.34
    }
  ]
}
```

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

## Feedback

Feedback APIs update node confidence/utility and source credibility without deleting history.

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/nodes/550e8400-e29b-41d4-a716-446655440000/validate \
  -H "Content-Type: application/json" \
  -d '{"reason": "verified externally"}'
```

Available actions:

| Path suffix | Effect |
|:------------|:-------|
| `/validate` | Increases claim confidence and validates the asserting source |
| `/refute` | Sets claim confidence low and refutes the asserting source |
| `/useful` | Increases utility and updates SM-2 recall metadata |
| `/stale` | Decreases confidence and utility |

`/useful` accepts `quality` from 0 to 5. `/refute` and `/stale` accept an optional `reason`.

**Response:**

```json
{
  "node_id": "550e8400-e29b-41d4-a716-446655440000",
  "action": "validated",
  "confidence": 1,
  "utility": 1,
  "source_id": "docs-crawler",
  "source_credibility": 0.67,
  "reason": "verified externally"
}
```

## Feedback Events

Feedback operations append durable audit events. List them with:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/feedback/events?after=2026-05-30T00:00:00Z"
```

**Response:**

```json
{
  "events": [
    {
      "event_id": "7ce69c7e-7f5b-4d23-86aa-a1b70f2fa111",
      "namespace": "my-app",
      "node_id": "550e8400-e29b-41d4-a716-446655440000",
      "node_version": 2,
      "action": "validated",
      "confidence": 1,
      "utility": 1,
      "source_id": "docs-crawler",
      "source_credibility": 0.67,
      "reason": "verified externally",
      "quality": 5,
      "tx_time": "2026-05-30T16:45:00Z"
    }
  ]
}
```

## Source Trust Timeline

Source trust timelines are derived from feedback events that changed source credibility:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/sources/docs-crawler/trust?after=2026-05-30T00:00:00Z"
```

**Response:**

```json
{
  "source_id": "docs-crawler",
  "points": [
    {
      "source_id": "docs-crawler",
      "node_id": "550e8400-e29b-41d4-a716-446655440000",
      "action": "validated",
      "source_credibility": 0.67,
      "tx_time": "2026-05-30T16:45:00Z"
    }
  ]
}
```

## Claim Review Queue

The review queue derives ranked operator tasks from feedback events, low-confidence claims, and contradiction edges:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/queue?after=2026-05-30T00:00:00Z&low_confidence_threshold=0.35&limit=20"
```

Query parameters:

| Parameter | Description |
|:----------|:------------|
| `after` | Optional RFC3339 timestamp for feedback-derived review items |
| `low_confidence_threshold` | Optional threshold for low-confidence claim tasks; defaults to `0.35` |
| `source_trust_threshold` | Optional latest source credibility threshold for source-trust anomaly tasks |
| `source_trust_drop_threshold` | Optional credibility drop threshold across the selected feedback window |
| `source_refutation_threshold` | Optional count threshold for repeated source refutations |
| `escalation_after_hours` | Optional age threshold that adds escalation metadata to overdue assigned, due snoozed, or high-priority source anomaly items |
| `source_anomaly_escalation_priority` | Optional priority threshold for escalating source-trust anomaly items; defaults to `0.9` when escalation is enabled |
| `type` | Optional comma-separated review item types, such as `low_confidence` or `source_trust_anomaly` |
| `source_id` | Optional source identifier filter |
| `status` | Optional workflow status filter; undecided items match `open` |
| `owner` | Optional workflow owner filter |
| `limit` | Optional maximum number of ranked tasks |
| `mode` | Optional namespace mode when opening the namespace |

**Response:**

```json
{
  "items": [
    {
      "id": "feedback:7ce69c7e-7f5b-4d23-86aa-a1b70f2fa111",
      "type": "refuted",
      "priority": 0.95,
      "reason": "operator disputed source",
      "node_id": "550e8400-e29b-41d4-a716-446655440000",
      "source_id": "docs-crawler",
      "action": "refuted",
      "created_at": "2026-05-30T16:45:00Z",
      "suggested_action": "review claim and decide whether to retract, supersede, or add counter-evidence",
      "confidence": 0.05,
      "status": "assigned",
      "owner": "alice",
      "decision": "needs_evidence",
      "note": "check source logs",
      "reviewed_at": "2026-05-30T17:10:00Z",
      "escalated": true,
      "escalation_level": "review_overdue",
      "escalation_reason": "assigned review has waited 72.0 hours",
      "escalation_age_hours": 72
    }
  ]
}
```

Source trust anomaly tasks use `type: "source_trust_anomaly"` and are derived from the same durable feedback events as the source trust timeline. They can be triggered by a configured credibility drop, a low latest credibility threshold, or repeated refutations:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/queue?source_trust_drop_threshold=0.2&source_refutation_threshold=2"
```

Use filters to focus operational review views:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/queue?type=source_trust_anomaly&source_id=docs-crawler&status=open"
curl "http://localhost:7701/v1/namespaces/my-app/review/queue?type=low_confidence&status=assigned&owner=alice"
```

Use the escalation digest when dashboards need a compact grouped summary instead of every item:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/escalations?escalation_after_hours=72"
```

The digest uses the same query parameters as the queue endpoint and groups escalated items by owner, source, item type, and escalation level:

```json
{
  "digest": {
    "total_escalated": 2,
    "groups": [
      {
        "owner": "alice",
        "source_id": "docs-crawler",
        "type": "low_confidence",
        "escalation_level": "review_overdue",
        "count": 2,
        "max_priority": 1.42,
        "max_age_hours": 96,
        "review_ids": ["low_confidence:550e8400-e29b-41d4-a716-446655440000"]
      }
    ]
  }
}
```

Record workflow state for a derived review item with:

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/review/decisions \
  -H 'Content-Type: application/json' \
  -d '{
    "review_id": "low_confidence:550e8400-e29b-41d4-a716-446655440000",
    "status": "assigned",
    "owner": "alice",
    "decision": "needs_evidence",
    "note": "check source logs"
  }'
```

Supported statuses are `open`, `assigned`, `resolved`, and `snoozed`. Snoozed items require `recheck_at`; resolved items are hidden from the derived queue, and snoozed items are hidden until their re-check time.

List the append-only decision log with:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/decisions?after=2026-05-30T00:00:00Z"
```

## Narrative Retrieval

```bash
curl http://localhost:7701/v1/namespaces/my-app/nodes/550e8400-e29b-41d4-a716-446655440000/narrative
```

Returns a structured explanation containing the target claim, summary, supporting evidence, contradictions, provenance, and confidence explanation.

## Knowledge Gaps

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/gaps \
  -H "Content-Type: application/json" \
  -d '{"top_k": 20, "min_gap_size": 0.5, "max_gaps": 10}'
```

Returns sparse semantic regions with nearest topics, centroid vectors, density, confidence, and temporal gap metadata.

## Knowledge Acquisition Plan

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/acquisition/plan \
  -H "Content-Type: application/json" \
  -d '{"budget": 5, "max_gaps": 3}'
```

Returns prioritized acquisition tasks derived from knowledge gaps, low-confidence claims, stale claims, and active contradictions.

**Response:**

```json
{
  "namespace": "my-app",
  "coverage_score": 0.72,
  "total_nodes": 42,
  "tasks": [
    {
      "id": "low_confidence:550e8400-e29b-41d4-a716-446655440000",
      "type": "low_confidence",
      "priority": 0.8,
      "description": "Low confidence claim needs supporting evidence: Deploys use manual copy rollout",
      "prompt": "Find independent evidence that validates or refutes the related claim, then apply feedback or ingest counter-evidence.",
      "related_node_ids": ["550e8400-e29b-41d4-a716-446655440000"]
    }
  ]
}
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

Or use Bearer token prefix (recommended for production):

```bash
curl -X POST http://localhost:7701/v1/namespaces/my-app/write \
  -H "Authorization: Bearer acme-corp:write:sk-secret" \
  -H "Content-Type: application/json" \
  -d '{"content": "...", "source_id": "..."}'
```

## Admin UI

The admin dashboard is served on the observe port (**7702**):

```bash
# Dashboard
open http://localhost:7702/admin/

# Admin stats API
curl http://localhost:7702/admin/api/stats
```

The dashboard displays ingest/retrieval counters, error rates, and links to metrics and profiling endpoints.

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
