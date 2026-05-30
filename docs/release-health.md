---
title: Release Health
---

# Release Health

Release health records the confidence gates used for each tagged release. The latest docs remain forward-looking, while this page keeps a compact audit trail for tests, docs, and operational readiness.

## Current Gate

Run these checks before tagging a release:

| Gate | Command or evidence | Current expectation |
|:-----|:--------------------|:--------------------|
| Unit and integration tests | `go test -count=1 ./...` | Required for every release |
| Docs build | `npm run docs:build` | Required for every release |
| Patch hygiene | `git diff --check` | Required for every release |
| Ranking corpus | `TestRepresentativeCorpusRankingGolden` in `./internal/retrieval` | Required before ranking changes |
| Badger restart durability | `TestDB_BadgerRestartDurability` in `./pkg/client` | Required before storage or embedded-mode changes |
| API contract surface | gRPC, REST, and GraphQL server tests | Required before public API changes |
| Race and soak lane | `go test -race ./...` plus long-running concurrency probes | Recommended before concurrency-heavy releases |

## Release Summary

| Release | Unit and integration | Docs build | Ranking | Durability | API contract | Notes |
|:--------|:---------------------|:-----------|:--------|:-----------|:-------------|:------|
| v0.14.0 | Passed | Passed | Corpus coverage present | Restart coverage present | CLI Norn helper tests added | Adds local Norn registration helper |
| v0.13.0 | Passed | Passed | Corpus coverage present | Restart coverage present | REST and GraphQL source anomaly coverage added | Adds source trust anomaly review tasks |
| v0.12.0 | Passed | Passed | Corpus coverage present | Restart coverage present | REST and GraphQL review decision coverage added | Adds durable review workflow decisions |
| v0.11.2 | Passed | Passed | Corpus coverage present | Restart coverage present | Existing contract tests present | Adds this release health page and docs wiring |
| v0.11.1 | Passed | Passed | Representative corpus golden test added | Restart coverage present | Existing contract tests present | Hardened ranking candidate pool |
| v0.11.0 | Passed | Passed | Golden fixtures present | Restart coverage present | REST and GraphQL explain-rank coverage expanded | Added graph evidence to explain-rank |
| v0.10.0 | Passed | Passed | Golden fixtures present | Restart coverage present | Existing contract tests present | Added backup marker doctor check |
| v0.9.0 | Passed | Passed | Golden fixtures present | Restart coverage present | REST and GraphQL acquisition plan coverage added | Added acquisition planner |
| v0.8.0 | Passed | Passed | Golden fixtures present | Restart coverage present | REST and GraphQL explain-rank coverage added | Added explain-rank API |
| v0.7.0 | Passed | Passed | Golden fixtures present | Restart coverage present | REST and GraphQL review queue coverage added | Added claim review queue |
| v0.6.0 | Passed | Passed | Golden fixtures present | Restart coverage present | REST and GraphQL trust timeline coverage added | Added source trust timeline |
| v0.5.0 | Passed | Passed | Golden fixtures present | Restart coverage present | REST and GraphQL feedback event coverage added | Added feedback event log |
| v0.4.1 | Passed | Passed | Golden fixtures present | Restart coverage present | Existing contract tests present | Added doctor sample-write probe |
| v0.4.0 | Passed | Passed | Golden fixtures added | Badger restart test added | gRPC contract and REST failure-path tests added | Added introspection and first doctor slice |

## Interpreting Status

`Passed` means the gate was run during the local release slice before the commit and tag. `Present` means the release contains an automated test that covers the area, even if the table points to the broad suite rather than a single command.

Race and soak checks are not yet mandatory because they are slower and more environment-sensitive. They remain a recommended gate for storage, federation, compaction, or concurrent retrieval changes.
