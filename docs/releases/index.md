---
title: Releases
---

# Releases

Release notes are the high-level map of what changed, why it matters, and which docs to read next. The main docs track the latest stable release; feature tables include the version where major surfaces were introduced.

| Release | Date | Theme |
|:--------|:-----|:------|
| [v0.46.0](v0.46.0) | 2026-05-31 | Review handoff retry backoff recommendations |
| [v0.45.0](v0.45.0) | 2026-05-31 | Review handoff retry execution |
| [v0.44.0](v0.44.0) | 2026-05-31 | Review handoff retry candidates |
| [v0.43.0](v0.43.0) | 2026-05-31 | Review handoff delivery receipts |
| [v0.42.0](v0.42.0) | 2026-05-31 | Review handoff webhook execution |
| [v0.41.0](v0.41.0) | 2026-05-31 | Review handoff webhook plans |
| [v0.40.0](v0.40.0) | 2026-05-30 | Review handoff feed |
| [v0.39.0](v0.39.0) | 2026-05-30 | Review escalation digest export |
| [v0.38.0](v0.38.0) | 2026-05-30 | Review escalation digest |
| [v0.37.0](v0.37.0) | 2026-05-30 | Review escalation rules |
| [v0.36.0](v0.36.0) | 2026-05-30 | Backup index metadata publish |
| [v0.35.0](v0.35.0) | 2026-05-30 | Norn manifest publish |
| [v0.34.0](v0.34.0) | 2026-05-30 | Lifecycle index diffs |
| [v0.33.0](v0.33.0) | 2026-05-30 | Lifecycle index verification |
| [v0.32.0](v0.32.0) | 2026-05-30 | Lifecycle manifest indexes |
| [v0.31.0](v0.31.0) | 2026-05-30 | Lifecycle deletion plans |
| [v0.30.0](v0.30.0) | 2026-05-30 | Lifecycle retention reporting |
| [v0.29.0](v0.29.0) | 2026-05-30 | Lifecycle summary verification |
| [v0.28.0](v0.28.0) | 2026-05-30 | Backup lifecycle bundle |
| [v0.27.0](v0.27.0) | 2026-05-30 | Promotion receipt verification |
| [v0.26.0](v0.26.0) | 2026-05-30 | Restore promotion receipts |
| [v0.25.0](v0.25.0) | 2026-05-30 | Restore promotion checklist |
| [v0.24.0](v0.24.0) | 2026-05-30 | Restore rehearsal command |
| [v0.23.0](v0.23.0) | 2026-05-30 | Backup manifest verification |
| [v0.22.0](v0.22.0) | 2026-05-30 | Backup artifact manifests |
| [v0.21.0](v0.21.0) | 2026-05-30 | Automated backup runbook |
| [v0.20.0](v0.20.0) | 2026-05-30 | Snapshot diff preview |
| [v0.19.0](v0.19.0) | 2026-05-30 | Snapshot backup marker |
| [v0.18.0](v0.18.0) | 2026-05-30 | Snapshot restore reports |
| [v0.17.0](v0.17.0) | 2026-05-30 | Snapshot backup and restore CLI |
| [v0.16.0](v0.16.0) | 2026-05-30 | Norn live drift check |
| [v0.15.0](v0.15.0) | 2026-05-30 | Review queue filters |
| [v0.14.0](v0.14.0) | 2026-05-30 | Local Norn registration helper |
| [v0.13.0](v0.13.0) | 2026-05-30 | Source trust anomaly review tasks |
| [v0.12.0](v0.12.0) | 2026-05-30 | Review workflow persistence |
| [v0.11.2](v0.11.2) | 2026-05-30 | Release health page and gate summary |
| [v0.11.1](v0.11.1) | 2026-05-30 | Representative corpus ranking coverage and candidate-pool hardening |
| [v0.11.0](v0.11.0) | 2026-05-30 | Graph evidence in explain-rank responses |
| [v0.10.0](v0.10.0) | 2026-05-30 | Doctor backup readiness marker checks |
| [v0.9.0](v0.9.0) | 2026-05-30 | Knowledge acquisition planner for gaps and weak claims |
| [v0.8.0](v0.8.0) | 2026-05-30 | Explain-rank APIs for score component comparisons |
| [v0.7.0](v0.7.0) | 2026-05-30 | Claim review queue for refuted, stale, low-confidence, and contradictory claims |
| [v0.6.0](v0.6.0) | 2026-05-30 | Source trust timeline from feedback events |
| [v0.5.0](v0.5.0) | 2026-05-30 | Durable feedback event log and public audit queries |
| [v0.4.1](v0.4.1) | 2026-05-30 | Opt-in doctor sample write/retrieve probe |
| [v0.4.0](v0.4.0) | 2026-05-30 | Version introspection, doctor checks, durability coverage, ranking golden tests, and API contracts |
| [v0.3.0](v0.3.0) | 2026-05-29 | Graph inspection, feedback loops, explainability, and non-breaking dedup |
| v0.2.0 | 2026-03-30 | Query optimization capabilities |
| v0.1.0 | 2026-03-29 | Initial tagged release |

## Documentation Versioning

The docs are currently versioned by release notes and feature tags rather than a full version switcher. That keeps the latest docs easy to maintain while still making it clear when major capabilities landed.

Use the Git tags for exact historical source:

```bash
git checkout v0.46.0
npm ci
npm run docs:build
```

Full multi-version docs would make sense once there are active supported release lines with incompatible APIs. For now, v0.46.0 is intentionally non-breaking, so tagged release notes are the clearer tool.
