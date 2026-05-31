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
| v0.38.0 | Passed | Passed | Corpus coverage present | Review escalation digest coverage added | Go SDK, REST, and GraphQL digest tests added | Adds grouped escalation summaries by owner, source, type, and escalation level |
| v0.37.0 | Passed | Passed | Corpus coverage present | Review escalation coverage added | Go SDK review queue escalation tests added | Adds escalation metadata for aged assigned/snoozed reviews and high-priority source anomalies |
| v0.36.0 | Passed | Passed | Corpus coverage present | Lifecycle index publish coverage added | CLI lifecycle index publish tests added | Adds dry-run-first backup catalog metadata publishing without backup contents |
| v0.35.0 | Passed | Passed | Corpus coverage present | Norn publish coverage added | CLI Norn publish tests added | Adds dry-run-first Norn manifest publishing with explicit HTTP execution |
| v0.34.0 | Passed | Passed | Corpus coverage present | Lifecycle index diff coverage added | CLI lifecycle index diff tests added | Adds bundle, retention decision, and artifact delta reporting between lifecycle manifest indexes |
| v0.33.0 | Passed | Passed | Corpus coverage present | Lifecycle index verification coverage added | CLI lifecycle index verify tests added | Adds artifact size and hash verification for lifecycle manifest indexes |
| v0.32.0 | Passed | Passed | Corpus coverage present | Lifecycle index coverage added | CLI lifecycle index tests added | Adds compact backup manifest indexes with artifact hashes |
| v0.31.0 | Passed | Passed | Corpus coverage present | Lifecycle delete-plan coverage added | CLI delete-plan tests added | Adds reviewable deletion-plan script output for pruneable backup lifecycle artifacts |
| v0.30.0 | Passed | Passed | Corpus coverage present | Lifecycle retention report coverage added | CLI retention report tests added | Adds dry-run retention reporting for backup lifecycle bundles |
| v0.29.0 | Passed | Passed | Corpus coverage present | Lifecycle verification coverage added | CLI lifecycle verification tests added | Adds lifecycle summary verification for backup artifact bundles |
| v0.28.0 | Passed | Passed | Corpus coverage present | Backup lifecycle runbook coverage added | Docs lifecycle script verified by docs build | Adds guarded full-chain backup lifecycle workflow |
| v0.27.0 | Passed | Passed | Corpus coverage present | Promotion receipt verification coverage added | CLI receipt verification tests added | Adds receipt-to-manifest verification for restore promotion artifacts |
| v0.26.0 | Passed | Passed | Corpus coverage present | Restore promotion receipt coverage added | CLI import receipt tests added | Adds promotion JSON receipts for snapshot imports |
| v0.25.0 | Passed | Passed | Corpus coverage present | Restore promotion checklist coverage added | CLI rehearsal report tests added | Adds rehearsal timestamp, target namespace, and recommended import command |
| v0.24.0 | Passed | Passed | Corpus coverage present | Restore rehearsal coverage added | CLI rehearsal tests added | Adds combined artifact verification and dry-run restore preflight |
| v0.23.0 | Passed | Passed | Corpus coverage present | Backup manifest verification coverage added | CLI verify tests added | Adds pre-restore checksum, size, and record-count verification |
| v0.22.0 | Passed | Passed | Corpus coverage present | Backup artifact manifest coverage added | CLI manifest tests added | Adds checksummed JSON sidecars for snapshot export artifacts |
| v0.21.0 | Passed | Passed | Corpus coverage present | Backup runbook documents restore preview and marker checks | Feature metadata docs updated | Adds scheduled backup runbook for launchd, systemd, doctor, and Norn pairing |
| v0.20.0 | Passed | Passed | Corpus coverage present | Snapshot diff preview coverage added | Client snapshot diff report tests added | Adds new, changed, and unchanged node counts for snapshot restore reports |
| v0.19.0 | Passed | Passed | Corpus coverage present | Snapshot marker coverage added | CLI backup marker test added | Adds export-side backup marker for doctor readiness |
| v0.18.0 | Passed | Passed | Corpus coverage present | Snapshot report coverage added | Client snapshot report tests added | Adds dry-run and import reports for snapshot restore counts |
| v0.17.0 | Passed | Passed | Corpus coverage present | Snapshot export/import coverage added | CLI and client snapshot tests added | Adds public snapshot backup/restore helpers and CLI dry-run validation |
| v0.16.0 | Passed | Passed | Corpus coverage present | Restart coverage present | CLI Norn drift tests added | Adds live Norn manifest drift reporting |
| v0.15.0 | Passed | Passed | Corpus coverage present | Restart coverage present | SDK, REST, and GraphQL review queue filter tests added | Adds review queue filters by type, source, status, and owner |
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
