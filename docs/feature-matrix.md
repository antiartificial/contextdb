---
title: Feature Matrix
---

# Feature Matrix

This matrix is the implementation contract for the current codebase. "Introduced" tracks the release where a public capability first became a documented surface.

| Area | Status | Introduced | Tags | Evidence |
|:-----|:-------|:-----------|:-----|:---------|
| Embedded mode | Implemented | v0.1 | `runtime` | `client.ModeEmbedded`, memory stores, Badger persistence |
| Standard mode | Implemented | v0.1 | `runtime` | Postgres graph/KV/event/vector stores and migrations |
| Remote mode | Implemented | v0.1 | `runtime` | JSON-over-gRPC remote stores |
| Scaled mode | Partial | v0.2 | `runtime`, `scale` | Config surface exists; Qdrant/Redis paths require integration setup |
| Score breakdown | Implemented | v0.3 | `inspectability`, `non-breaking` | SDK, REST, gRPC, GraphQL expose weighted score contributions |
| Write deduplication | Implemented, opt-in | v0.3 | `cost`, `non-breaking` | Content fingerprints skip repeat embedding and touch existing nodes when enabled per request or server |
| GraphQL search | Implemented | v0.3 | `inspectability`, `product-surface` | `/graphql` search, filters, edge and source resolvers |
| Source credibility | Implemented | v0.1 | `epistemics` | Beta credibility model, labels, admission gate |
| Conflict detection | Implemented | v0.1 | `epistemics` | Write-time semantic contradiction tracking |
| Narrative retrieval | Implemented | v0.3 | `epistemics`, `inspectability` | Go SDK, REST, and GraphQL expose structured narrative reports |
| Knowledge gaps | Implemented | v0.3 | `epistemics`, `active-learning` | Go SDK, REST, and GraphQL expose gap reports |
| Knowledge acquisition planner | Implemented | v0.9 | `active-learning`, `epistemics`, `operations` | Go SDK, REST, and GraphQL expose prioritized acquisition tasks from gaps and weak claims |
| Feedback APIs | Implemented | v0.3 | `feedback-loop`, `non-breaking` | Go SDK, REST, gRPC, and GraphQL expose validate/refute/useful/stale |
| Explain-rank | Implemented | v0.8 | `inspectability`, `ranking`, `non-breaking` | Go SDK, REST, and GraphQL compare two nodes with score component deltas |
| Explain-rank graph evidence | Implemented | v0.11 | `inspectability`, `ranking`, `graph` | Explain-rank responses include support-chain evidence and compound confidence |
| Feedback event log | Implemented | v0.5 | `feedback-loop`, `audit`, `non-breaking` | Go SDK, REST, and GraphQL expose durable feedback events |
| Source trust timeline | Implemented | v0.6 | `audit`, `epistemics`, `feedback-loop` | Go SDK, REST, and GraphQL expose credibility points from feedback events |
| Claim review queue | Implemented | v0.7 | `feedback-loop`, `epistemics`, `operations` | Go SDK, REST, and GraphQL expose ranked review tasks for refuted, stale, low-confidence, and contradictory claims |
| Review workflow persistence | Implemented | v0.12 | `feedback-loop`, `operations`, `audit` | Go SDK, REST, and GraphQL expose append-only review decisions for assignment, snooze, resolution, and notes |
| Source trust anomaly alerts | Implemented | v0.13 | `feedback-loop`, `epistemics`, `operations` | Review queues emit source-trust anomaly tasks for credibility drops, low trust, and repeated refutations |
| Review queue filters | Implemented | v0.15 | `feedback-loop`, `operations`, `inspectability` | Go SDK, REST, and GraphQL filter review queues by task type, source, workflow status, and owner |
| Version and feature introspection | Implemented | v0.4 | `introspection`, `non-breaking` | REST `/v1/version`, `/v1/features`, `/v1/migrations`; GraphQL `version`, `features`, `migrations` |
| `contextdb doctor` | Implemented, non-mutating checks | v0.4 | `operations`, `introspection` | CLI checks live REST ping, version, features, and migration metadata |
| `contextdb doctor --sample-write` | Implemented, opt-in mutating probe | v0.4.1 | `operations`, `durability` | CLI writes a deduplicated probe node and verifies vector retrieval sees it |
| `contextdb doctor --backup-marker` | Implemented, opt-in readiness check | v0.10 | `operations`, `backup`, `durability` | CLI verifies a backup marker file exists and is newer than `--max-backup-age` |
| Snapshot backup/restore | Implemented | v0.17 | `operations`, `backup`, `durability` | Go client and `contextdb snapshot export/import` provide NDJSON backup, seeded export filters, namespace override, and import dry-run validation |
| Snapshot restore reports | Implemented | v0.18 | `operations`, `backup`, `inspectability` | Go client report helpers and `contextdb snapshot import --report` summarize lines, records, vectors, and namespace overrides |
| Snapshot backup marker | Implemented | v0.19 | `operations`, `backup`, `durability` | `contextdb snapshot export --backup-marker` writes a doctor-compatible marker only after export succeeds |
| Snapshot diff preview | Implemented | v0.20 | `operations`, `backup`, `inspectability` | Snapshot restore reports classify nodes as new, changed, or unchanged during dry-run and import |
| Backup runbook | Implemented | v0.21 | `operations`, `backup`, `deployment` | Deployment docs provide scheduled export, restore preview, marker, doctor, launchd, systemd, and Norn check flow |
| Backup artifact manifest | Implemented | v0.22 | `operations`, `backup`, `audit` | `contextdb snapshot export --manifest` writes a checksummed JSON sidecar with namespace, version, bytes, marker path, and record counts |
| Backup manifest verify | Implemented | v0.23 | `operations`, `backup`, `audit` | `contextdb snapshot verify --manifest --in` validates checksum, size, and record counts before restore |
| Restore rehearsal | Implemented | v0.24 | `operations`, `backup`, `audit` | `contextdb snapshot rehearse --manifest --in --namespace` verifies the artifact and returns dry-run restore counts in one preflight report |
| Restore promotion checklist | Implemented | v0.25 | `operations`, `backup`, `audit` | Rehearsal reports include timestamp, target namespace, and a shell-quoted recommended import command |
| Restore promotion receipt | Implemented | v0.26 | `operations`, `backup`, `audit` | `contextdb snapshot import --promotion-report --promotion-note` writes a JSON receipt with import counts after successful promotion |
| Release health page | Implemented | v0.11.2 | `operations`, `release`, `durability` | Docs page records unit, docs-build, ranking, durability, API contract, and race/soak release gates |
| Durability and ranking tests | Implemented | v0.4 | `durability`, `ranking` | Badger restart test, ranking golden fixtures, representative corpus ranking coverage, gRPC contract test, REST failure-path coverage |
| Mini/Norn deployment notes | Implemented | v0.3 | `operations` | Internal live deployment discovery and health-check docs |
| Norn registration helper | Implemented | v0.14 | `operations`, `deployment` | `contextdb norn manifest` and `contextdb norn validate` generate and check service entries |
| Norn live drift check | Implemented | v0.16 | `operations`, `deployment` | `contextdb norn drift` compares expected local service metadata with the live Norn manifest and reports field differences |
| Admin/debug UI | Not started | Future | `inspectability` | GraphQL now exposes the data needed for an inspector |

## Next Candidates

1. A local belief debugger UI backed by GraphQL, feature introspection, explain-rank, feedback events, and source trust timelines.
2. Review escalation rules for aged assigned, snoozed, or high-severity source anomaly items.
3. Deeper doctor store/index consistency checks.
