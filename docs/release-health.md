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
| v0.67.0 | Passed | Passed | Corpus coverage present | KV derivation recipe docs added | Docs build coverage | Adds naming, review, and promotion recipes for derived recent-node KV cache values |
| v0.66.0 | Passed | Passed | Corpus coverage present | Published backup repair hint coverage added | CLI drift and doctor hint tests added | Adds dry-run publish command hints to published backup drift reports |
| v0.65.0 | Passed | Passed | Corpus coverage present | Cookbook docs added | Docs build coverage | Adds retry fatigue owner and escalation-lane cookbook examples for handoff routing |
| v0.64.0 | Passed | Passed | Baseline retention coverage added | Corpus coverage present | CLI ranking baseline retention tests added | Adds read-only retained/current/pruneable reports for versioned ranking baselines |
| v0.63.0 | Passed | Passed | Corpus coverage present | KV derived value coverage added | CLI KV derived recent-node tests added | Adds dry-run-first recent-node session context derivation for KV cache repair |
| v0.62.0 | Passed | Passed | Corpus coverage present | Published backup drift doctor coverage added | CLI doctor published drift tests added | Adds opt-in local-vs-published backup catalog drift checks to combined doctor reports |
| v0.61.0 | Passed | Passed | Baseline policy coverage added | Corpus coverage present | CLI ranking baseline tests added | Adds versioned ranking baseline artifacts and previous-baseline resolution |
| v0.60.0 | Passed | Passed | Corpus coverage present | Retry fatigue filter coverage added | Go SDK, REST, and GraphQL fatigue filter tests added | Adds owner and escalation filters for retry fatigue handoffs |
| v0.59.0 | Passed | Passed | Corpus coverage present | KV refresh repair coverage added | CLI KV cache repair tests added | Adds dry-run-first reviewed KV hot-key refresh execution |
| v0.58.0 | Passed | Passed | Corpus coverage present | Retry fatigue owner grouping coverage added | Go SDK, REST, and GraphQL fatigue grouping tests added | Adds owner and escalation breakdowns to retry fatigue handoffs |
| v0.57.0 | Passed | Passed | Corpus coverage present | Published backup freshness doctor coverage added | CLI doctor published freshness tests added | Adds opt-in published backup catalog freshness to combined doctor reports |
| v0.56.0 | Passed | Passed | Historical diff coverage added | Corpus coverage present | CLI ranking eval diff test added | Adds release-to-release rank and top-score movement summaries for representative corpus ranking |
| v0.55.0 | Passed | Passed | Markdown recap coverage added | Corpus coverage present | CLI ranking eval Markdown test added | Adds compact Markdown release-review summaries for representative corpus ranking |
| v0.54.0 | Passed | Passed | Corpus coverage present | KV sampling coverage added | CLI doctor KV consistency tests added | Adds opt-in KV hot-key sampling and refresh candidate reporting |
| v0.53.0 | Passed | Passed | Corpus coverage present | Retry fatigue Markdown coverage added | Go SDK and REST Markdown export tests added | Adds Markdown incident handoff export for endpoint-level retry fatigue |
| v0.52.0 | Passed | Passed | Corpus coverage present | Publish freshness coverage added | CLI publish freshness tests added | Adds read-only freshness checks for published backup catalog metadata |
| v0.51.0 | Passed | Passed | Corpus coverage present | Retry fatigue coverage added | Go SDK, REST, and GraphQL retry fatigue tests added | Adds read-only endpoint-level fatigue summaries for failed review handoff retries |
| v0.50.0 | Passed | Passed | Corpus coverage present | Vector repair coverage added | CLI vector repair report tests added | Adds dry-run-first vector index repair execution for reviewed rebuild candidates |
| v0.49.0 | Passed | Passed | Corpus coverage present | Store consistency coverage added | CLI doctor store consistency tests added | Adds opt-in local doctor checks for fingerprint lookups and vector rebuild candidates |
| v0.48.0 | Passed | Passed | Ranking eval snapshot coverage added | Restart coverage present | CLI ranking eval snapshot test added | Adds JSON score-drift reports for the representative corpus |
| v0.47.0 | Passed | Passed | Corpus coverage present | Lifecycle index publish drift coverage added | CLI lifecycle index publish drift tests added | Adds dry-run comparison between local and published backup catalog metadata |
| v0.46.0 | Passed | Passed | Corpus coverage present | Review handoff retry backoff coverage added | Go SDK, REST, and GraphQL retry recommendation tests added | Adds read-only retry pacing recommendations from delivery receipt history |
| v0.45.0 | Passed | Passed | Corpus coverage present | Review handoff retry execution coverage added | Go SDK, REST, and GraphQL retry execution tests added | Adds explicit operator-triggered resend for unresolved failed handoff deliveries |
| v0.44.0 | Passed | Passed | Corpus coverage present | Review handoff retry candidate coverage added | Go SDK, REST, and GraphQL retry candidate tests added | Adds read-only retry candidates for unresolved failed handoff deliveries |
| v0.43.0 | Passed | Passed | Corpus coverage present | Review handoff delivery receipt coverage added | Go SDK, REST, and GraphQL receipt tests added | Adds append-only webhook delivery receipts with payload and response hashes |
| v0.42.0 | Passed | Passed | Corpus coverage present | Review handoff webhook execution coverage added | Go SDK, REST, and GraphQL execution tests added | Adds explicit synchronous webhook delivery with timeout and response capture |
| v0.41.0 | Passed | Passed | Corpus coverage present | Review handoff webhook plan coverage added | Go SDK, REST, and GraphQL webhook plan tests added | Adds signed dry-run webhook delivery plans for saved review handoffs |
| v0.40.0 | Passed | Passed | Corpus coverage present | Review handoff feed coverage added | Go SDK, REST, and GraphQL handoff feed tests added | Adds polling-friendly handoff feeds filtered by owner and escalation level |
| v0.39.0 | Passed | Passed | Corpus coverage present | Review escalation digest export coverage added | Go SDK, REST, and GraphQL digest snapshot tests added | Adds durable escalation digest snapshots for review handoffs |
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
