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
| Feedback APIs | Implemented | v0.3 | `feedback-loop`, `non-breaking` | Go SDK, REST, gRPC, and GraphQL expose validate/refute/useful/stale |
| Version and feature introspection | Implemented | v0.4 | `introspection`, `non-breaking` | REST `/v1/version`, `/v1/features`, `/v1/migrations`; GraphQL `version`, `features`, `migrations` |
| Durability and ranking tests | Implemented | v0.4 | `durability`, `ranking` | Badger restart test, ranking golden fixtures, gRPC contract test, REST failure-path coverage |
| Mini/Norn deployment notes | Implemented | v0.3 | `operations` | Internal live deployment discovery and health-check docs |
| Admin/debug UI | Not started | Future | `inspectability` | GraphQL now exposes the data needed for an inspector |

## Next Candidates

1. A local belief debugger UI backed by GraphQL and feature introspection.
2. A `contextdb doctor` command that checks live version, migrations, sample writes, and vector retrieval.
3. Optional default-on dedup in a future major version, if desired.
