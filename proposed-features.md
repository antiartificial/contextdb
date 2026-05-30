# Proposed Features

This is the working backlog for features that would make contextdb more useful, inspectable, and durable as a live system.

## Product And Inspection

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| Belief debugger UI | Makes nodes, score breakdowns, evidence, contradictions, source trust, and history visible in one place | Back it with the existing GraphQL surface |
| Ranking evaluation dashboard | Tracks query sets, expected nodes, recall@k, MRR, and score deltas across releases | Useful before changing score weights or fusion logic |
| Explain-rank endpoint | Answers "why did this node rank above that one?" | Combine score breakdown, source credibility, recency, utility, and graph path evidence |
| Feature/version introspection | Lets clients ask which APIs and migrations are available | Add `/v1/version`, `/v1/features`, `/v1/migrations`, plus GraphQL equivalents |
| Local Norn registration helper | Reduces drift between live services and docs | Generate or validate a Norn manifest entry for contextdb |

## Feedback And Epistemics

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| Feedback event log | Makes validate/refute/useful/stale auditable as explicit events | Keep current node property updates, but add durable event records |
| Claim review queue | Turns contradictions, low confidence, and stale claims into operator tasks | Feed from conflict detection, active learning, and expiry signals |
| Source trust timeline | Shows how source credibility changed over time | Useful for moderation, incident reviews, and agent trust tuning |
| Knowledge acquisition planner | Converts knowledge gaps into suggested crawl/search/research tasks | Natural next step after gap detection |

## Durability And Operations

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| `contextdb doctor` | One command to verify stores, migrations, indexes, health, and sample writes | Should run locally and against live deployments |
| Release health page | Makes release confidence visible | Document unit, integration, durability, ranking, and API parity status per release |
| Backup/restore command | Productizes snapshot import/export | Include dry-run validation and namespace filters |
| Store repair/index rebuild | Helps recover from vector index or KV drift | Especially useful for embedded Badger deployments |
| Soak/race test lane | Catches concurrency and long-running drift | Run `go test -race ./...` plus concurrent writers/readers/feedback loops |

## Test Investments

Priority additions:

1. Restart durability suite for Badger-backed embedded mode.
2. Docker-backed Postgres integration suite for migrations, fingerprint indexes, feedback, and vector retrieval.
3. Ranking golden tests for namespace modes and representative corpora.
4. API contract parity tests across Go SDK, REST, gRPC, and GraphQL.
5. Failure injection for unavailable vector stores, graph stores, embedders, and malformed API requests.
6. Long-running race/soak tests for concurrent writes, reads, feedback, dedup, and compaction.

## Versioning Approach

The current docs should stay latest-first, with release recap pages and feature tags. Full multi-version docs become worthwhile once there are multiple supported release lines with incompatible APIs.
