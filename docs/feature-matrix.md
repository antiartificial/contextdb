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
| Version and feature introspection | Implemented | v0.4 | `introspection`, `non-breaking` | REST `/v1/version`, `/v1/features`, `/v1/migrations`; GraphQL `version`, `features`, `migrations` |
| `contextdb doctor` | Implemented, non-mutating checks | v0.4 | `operations`, `introspection` | CLI checks live REST ping, version, features, and migration metadata |
| `contextdb doctor --sample-write` | Implemented, opt-in mutating probe | v0.4.1 | `operations`, `durability` | CLI writes a deduplicated probe node and verifies vector retrieval sees it |
| `contextdb doctor --backup-marker` | Implemented, opt-in readiness check | v0.10 | `operations`, `backup`, `durability` | CLI verifies a backup marker file exists and is newer than `--max-backup-age` |
| Durability and ranking tests | Implemented | v0.4 | `durability`, `ranking` | Badger restart test, ranking golden fixtures, gRPC contract test, REST failure-path coverage |
| Mini/Norn deployment notes | Implemented | v0.3 | `operations` | Internal live deployment discovery and health-check docs |
| Admin/debug UI | Not started | Future | `inspectability` | GraphQL now exposes the data needed for an inspector |

## Next Candidates

1. A local belief debugger UI backed by GraphQL, feature introspection, explain-rank, feedback events, and source trust timelines.
2. Review workflow persistence for assignment, status, resolution notes, and re-check scheduling.
3. Deeper doctor store/index consistency checks.
