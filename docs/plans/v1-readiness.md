---
title: v1 readiness gates
---

# v1 readiness gates

This is the release decision record for a future `v1.0.0`, not a release claim. The current package version remains `v0.123.0`. A green test run establishes only the behavior it exercises; deployment and rollback evidence must be collected for the candidate actually released.

## Proposed supported contract

| Surface | v1 contract to freeze |
|:--|:--|
| Storage | Embedded Badger and Postgres standard mode. In-memory mode remains useful for tests and development. Scaled mode is unavailable. |
| Access | Go SDK, REST `/v1`, GraphQL, and JSON-over-gRPC remote access. Keep existing temporal, source, and score fields in the response contracts. |
| Query time | `ValidAt` is effective time; `KnownAt` is knowledge time. Both default according to the current API contract. Historical answers select the version known at `KnownAt` and then test validity at `ValidAt`. |
| Provenance | Deduplication remains opt-in. Every independent source assertion can be inspected and retracted independently. The final assertion retraction creates one node version and one audit edge atomically. |
| Compatibility | Document supported Go, Python, and TypeScript SDK versions, Postgres major versions and extensions, wire compatibility, migration policy, and rollback before tagging v1. |

## Gate status

| Gate | Status | Evidence and closure condition |
|:--|:--|:--|
| Historical and provenance correctness | **Partly closed** | Memory/Badger conformance covers atomic final-source node/audit updates and stale-version rejection. Postgres integration forces an audit-edge conflict and confirms node rollback; existing tests cover concurrent source corrections and historical edge walks. Still run repeated concurrent/fault trials and restore tests on a release candidate. |
| Retrieval quality and scale | **Open** | The 13-query representative fixture currently passes 13/13 with MRR@5 0.897; the seven-case P0 temporal/source fixture remains a regression check. Migration 004 adds a Postgres trigram candidate index over every node version; integration tests cover old-version retrieval and literal matching. These small fixtures do not set realistic quality or latency thresholds. Add a larger sourced corpus, explicit evidence-completeness scoring, backend parity, and repeated p50/p95 latency and index-size measurements. Badger lexical search still scans visible node IDs. |
| Public contract | **Open** | The README now agrees with the feature matrix that Scaled mode is unavailable. Freeze the exact SDK/API/schema compatibility and deprecation policy, then add versioned contract tests for the selected surfaces. The proposed table above is a starting point, not a compatibility guarantee. |
| Release and upgrade | **Partly closed** | A Postgres integration test upgrades an isolated pre-004 schema and queries a pre-upgrade node through the indexed path. CI runs it alongside the Postgres lifecycle tests. Before tagging, rehearse backup, migration, restore, and rollback on a production-like copy; run doctor and API probes against the candidate deployment; capture artifacts and verify the image/tag and docs version match. |

## Work to finish, in order

1. Run a production-like Postgres migration and restore rehearsal on a copied dataset. Record starting schema/version, backup manifest, migration result, query parity, rollback path, and final doctor output.
2. Build a sourced retrieval corpus with identifiers, corrected claims, conflicting sources, late-arriving evidence, long namespaces, and explicit answer/evidence judgments. Set pass criteria for accuracy, evidence completeness, and latency before tuning ranking.
3. Index Badger lexical candidates or document a measured namespace-size bound for embedded v1 use. Measure Postgres trigram query plans, p50/p95 latency, and index growth over repeated runs at realistic sizes.
4. Freeze the access, SDK, Postgres, migration, and deployment contract. Check examples and feature introspection against those choices.
5. Only then cut and deploy a v1 release candidate, verify health and restore/rollback, and tag `v1.0.0` after review.
