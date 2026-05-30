# Proposed Features

This is the working backlog for features that would make contextdb more useful, inspectable, and durable as a live system.

## Completed In v0.4.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Feature/version introspection | Implemented | REST `/v1/version`, `/v1/features`, `/v1/migrations`; GraphQL `version`, `features`, `migrations` |
| `contextdb doctor` | First slice implemented | Non-mutating CLI checks live REST ping, version, features, and migrations |
| Restart durability test | Implemented | Badger-backed embedded restart test covers nodes, vectors, history, feedback, and dedup |
| Ranking golden tests | Implemented | Belief-system and agent-memory score ordering fixtures |
| API contract test | Implemented | gRPC write/retrieve/feedback contract and REST invalid-node-ID failure path |

## Completed In v0.4.1

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| `contextdb doctor --sample-write` | Implemented | Opt-in CLI probe writes a deduplicated `DoctorProbe` node and verifies vector retrieval returns it |

## Completed In v0.5.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Feedback event log | Implemented | Feedback operations append durable events; Go SDK, REST, and GraphQL expose feedback event history |

## Completed In v0.6.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Source trust timeline | Implemented | Go SDK, REST, and GraphQL expose source credibility points derived from feedback events |

## Completed In v0.7.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Claim review queue | Implemented | Go SDK, REST, and GraphQL expose ranked tasks for refuted, stale, low-confidence, and contradictory claims |

## Completed In v0.8.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Explain-rank endpoint | Implemented | Go SDK, REST, and GraphQL compare two nodes and expose score component deltas |

## Completed In v0.9.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Knowledge acquisition planner | Implemented | Go SDK, REST, and GraphQL convert knowledge gaps and weak claims into prioritized acquisition tasks |

## Completed In v0.10.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Doctor backup readiness | Implemented | `contextdb doctor --backup-marker PATH --max-backup-age 24h` adds a `backup_readiness` check |

## Completed In v0.11.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Explain-rank graph evidence | Implemented | Explain-rank responses include support-chain links, edge weights, and compound confidence through Go SDK, REST, and GraphQL |

## Product And Inspection

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| Belief debugger UI | Makes nodes, score breakdowns, evidence, contradictions, source trust, and history visible in one place | Back it with the existing GraphQL surface |
| Ranking evaluation dashboard | Tracks query sets, expected nodes, recall@k, MRR, and score deltas across releases | Useful before changing score weights or fusion logic |
| Explain-rank endpoint | Answers "why did this node rank above that one?" | Completed in v0.8.0; graph support-chain evidence completed in v0.11.0; next step is UI integration |
| Feature/version introspection | Lets clients ask which APIs and migrations are available | Completed in v0.4.0; keep expanding feature metadata as APIs mature |
| Local Norn registration helper | Reduces drift between live services and docs | Generate or validate a Norn manifest entry for contextdb |

## Feedback And Epistemics

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| Feedback event log | Makes validate/refute/useful/stale auditable as explicit events | Completed in v0.5.0; next step is source trust timeline views |
| Claim review queue | Turns contradictions, low confidence, and stale claims into operator tasks | Completed in v0.7.0; next step is persistence for status, assignment, and resolution notes |
| Source trust timeline | Shows how source credibility changed over time | Completed in v0.6.0; next step is richer timeline visualization in the debugger UI |
| Knowledge acquisition planner | Converts knowledge gaps into suggested crawl/search/research tasks | Completed in v0.9.0; next step is connector-specific acquisition execution |
| Review workflow persistence | Tracks review status, owners, decisions, and re-check schedules | Builds on the derived queue without making queue generation stateful |

## Durability And Operations

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| `contextdb doctor` | One command to verify stores, migrations, indexes, health, and sample writes | Non-mutating checks completed in v0.4.0; opt-in sample write/retrieve probe completed in v0.4.1; deeper store/index checks remain |
| Release health page | Makes release confidence visible | Document unit, integration, durability, ranking, and API parity status per release |
| Backup/restore command | Productizes snapshot import/export | Include dry-run validation and namespace filters |
| Store repair/index rebuild | Helps recover from vector index or KV drift | Especially useful for embedded Badger deployments |
| Soak/race test lane | Catches concurrency and long-running drift | Run `go test -race ./...` plus concurrent writers/readers/feedback loops |

## Test Investments

Priority additions:

1. Restart durability suite for Badger-backed embedded mode. Implemented for the core write/feedback/dedup/retrieve restart path in v0.4.0.
2. Docker-backed Postgres integration suite for migrations, fingerprint indexes, feedback, and vector retrieval.
3. Ranking golden tests for namespace modes and representative corpora. Implemented for belief-system and agent-memory presets in v0.4.0; representative corpora are still next.
4. API contract parity tests across Go SDK, REST, gRPC, and GraphQL. Expanded in v0.4.0 with gRPC public contract and REST failure-path coverage.
5. Failure injection for unavailable vector stores, graph stores, embedders, and malformed API requests.
6. Long-running race/soak tests for concurrent writes, reads, feedback, dedup, and compaction.

## Versioning Approach

The current docs should stay latest-first, with release recap pages and feature tags. Full multi-version docs become worthwhile once there are multiple supported release lines with incompatible APIs.

## Likely Next Features

| Feature | Why it belongs next | First useful slice |
|:--------|:--------------------|:-------------------|
| Belief debugger UI | GraphQL plus introspection gives a stable product surface for an inspection tool | Read-only local UI for search results, explain-rank comparisons, sources, edges, and narrative reports |
| Release health page | The release process now has concrete test categories to report | Add a docs page that lists unit, durability, ranking, API contract, docs-build, and race-test status per release |
| Explain-rank graph evidence | The first explain-rank slice covers score deltas; graph-aware evidence makes explanations deeper | Support-chain evidence completed in v0.11.0; source trust context and contradiction path summaries remain useful next steps |
| Doctor backup readiness | The doctor command now has live metadata and write/read checks; backup checks make it more operationally complete | Completed in v0.10.0; deeper store/index consistency checks remain |
| Review workflow persistence | The derived queue now exists; operators need durable triage state around it | Add assigned/resolved/snoozed metadata and an append-only review decision log |
| Source trust anomaly alerts | Trust timelines now exist; the next step is detecting suspicious credibility drops or repeated refutations | Emit review tasks when a source crosses configured credibility thresholds |
| Acquisition execution connectors | Planner tasks now exist; the next step is executing them through configured crawlers/search tools | Add connector hooks and dry-run previews for source-constrained acquisition tasks |
| Postgres integration harness | Standard mode needs the same confidence now covered for Badger restarts | Docker-backed test for migrations, fingerprint dedup, feedback, and vector retrieval |
