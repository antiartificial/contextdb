---
title: Feature Matrix
---

# Feature Matrix

This matrix is the implementation contract for the current codebase.

| Area | Status | Evidence |
|:-----|:-------|:---------|
| Embedded mode | Implemented | `client.ModeEmbedded`, memory stores, Badger persistence |
| Standard mode | Implemented | Postgres graph/KV/event/vector stores and migrations |
| Remote mode | Implemented | JSON-over-gRPC remote stores |
| Scaled mode | Partial | Config surface exists; Qdrant/Redis paths require integration setup |
| Score breakdown | Implemented | SDK, REST, gRPC, GraphQL expose weighted score contributions |
| Write deduplication | Implemented, opt-in | Content fingerprints skip repeat embedding and touch existing nodes when enabled per request or server |
| GraphQL search | Implemented | `/graphql` search, filters, edge and source resolvers |
| Source credibility | Implemented | Beta credibility model, labels, admission gate |
| Conflict detection | Implemented | Write-time semantic contradiction tracking |
| Narrative retrieval | Implemented | Go SDK, REST, and GraphQL expose structured narrative reports |
| Knowledge gaps | Implemented | Go SDK, REST, and GraphQL expose gap reports |
| Feedback APIs | Implemented | Go SDK, REST, gRPC, and GraphQL expose validate/refute/useful/stale |
| Admin/debug UI | Not started | GraphQL now exposes the data needed for an inspector |

## Next Candidates

1. A local belief debugger UI backed by GraphQL.
2. Release/version policy cleanup across Go tags and package metadata.
3. Optional default-on dedup in a future major version, if desired.
