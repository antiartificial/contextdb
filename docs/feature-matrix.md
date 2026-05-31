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
| Review workflow persistence | Implemented | v0.12 | `feedback-loop`, `operations`, `audit` | Go SDK, REST, and GraphQL expose append-only review decisions for assignment, snooze, resolution, and notes |
| Source trust anomaly alerts | Implemented | v0.13 | `feedback-loop`, `epistemics`, `operations` | Review queues emit source-trust anomaly tasks for credibility drops, low trust, and repeated refutations |
| Review queue filters | Implemented | v0.15 | `feedback-loop`, `operations`, `inspectability` | Go SDK, REST, and GraphQL filter review queues by task type, source, workflow status, and owner |
| Version and feature introspection | Implemented | v0.4 | `introspection`, `non-breaking` | REST `/v1/version`, `/v1/features`, `/v1/migrations`; GraphQL `version`, `features`, `migrations` |
| `contextdb doctor` | Implemented, non-mutating checks | v0.4 | `operations`, `introspection` | CLI checks live REST ping, version, features, and migration metadata |
| `contextdb doctor --sample-write` | Implemented, opt-in mutating probe | v0.4.1 | `operations`, `durability` | CLI writes a deduplicated probe node and verifies vector retrieval sees it |
| `contextdb doctor --backup-marker` | Implemented, opt-in readiness check | v0.10 | `operations`, `backup`, `durability` | CLI verifies a backup marker file exists and is newer than `--max-backup-age` |
| `contextdb doctor --store-consistency` | Implemented, opt-in local check | v0.49 | `operations`, `durability` | CLI samples valid graph nodes, checks fingerprint lookup, and reports vector rebuild candidates |
| `contextdb doctor --kv-key` | Implemented, opt-in local check | v0.54 | `operations`, `durability` | CLI samples expected KV hot keys and reports missing cache refresh candidates |
| `contextdb doctor --kv-derived-key` | Implemented, opt-in local check | v0.71 | `operations`, `durability` | CLI validates derived KV metadata and warns when `generated_at` is older than `--max-kv-derived-age` |
| `kv_derived_freshness` repair hint | Implemented | v0.75 | `operations`, `durability` | Doctor failures include a dry-run `repair kv-cache --derive recent-nodes` command hint |
| `contextdb doctor --published-backup-url` | Implemented, opt-in readiness check | v0.57 | `operations`, `backup`, `durability` | CLI reuses published backup catalog freshness checks inside the combined doctor report |
| `contextdb doctor --published-backup-index` | Implemented, opt-in readiness check | v0.62 | `operations`, `backup`, `durability` | CLI reuses published backup catalog drift checks inside the combined doctor report |
| `contextdb doctor --published-backup-receipt` | Implemented, opt-in readiness check | v0.82 | `operations`, `backup`, `audit` | CLI verifies published backup repair receipts against the local lifecycle index inside the combined doctor report |
| Doctor backup receipt runbook lane | Implemented | v0.95 | `operations`, `backup`, `audit` | Published backup repair docs now include a freshness, drift, receipt, doctor, and final closeout lane |
| Doctor backup receipt closure artifact bundle | Implemented | v0.98 | `operations`, `backup`, `audit` | Published backup repair docs define stable artifact filenames for dry-run, execute, receipt, receipt-check, and final doctor reports |
| `published_backup_drift` repair hint | Implemented | v0.66 | `operations`, `backup`, `durability` | Drift reports include a dry-run publish command hint when local catalog metadata should replace published metadata |
| Published backup repair guard | Implemented | v0.70 | `operations`, `backup`, `durability` | Deployment docs provide safety checks before executing published backup catalog replacement |
| Published backup repair receipt | Implemented | v0.74 | `operations`, `backup`, `audit` | `snapshot lifecycle index publish --execute --receipt-out` writes durable evidence for catalog replacement |
| Published backup repair receipt verify | Implemented | v0.78 | `operations`, `backup`, `audit` | `snapshot lifecycle index publish receipt verify --receipt --in` compares repair receipts against local lifecycle index payloads |
| `contextdb repair vector-index` | Implemented, dry-run first | v0.50 | `operations`, `durability` | CLI reports vector rebuild candidates and reindexes reviewed graph-node vectors only with `--execute` |
| `contextdb repair kv-cache` | Implemented, dry-run first | v0.59 | `operations`, `durability` | CLI plans reviewed KV hot-key refreshes and writes explicit values only with `--execute` |
| `contextdb repair kv-cache --derive recent-nodes` | Implemented, dry-run first | v0.63 | `operations`, `durability` | CLI derives reviewed recent-node session context values from graph data before optional KV writes |
| KV derivation recipes | Implemented | v0.67 | `operations`, `durability`, `deployment` | Deployment docs provide naming, review, and promotion recipes for derived recent-node KV cache values |
| KV derived repair execution recipe | Implemented | v0.79 | `operations`, `durability`, `deployment` | Deployment docs provide a guarded doctor-hint to dry-run to execute checklist for stale derived KV refreshes |
| KV derived refresh receipt | Implemented | v0.83 | `operations`, `durability`, `audit` | `repair kv-cache --derive recent-nodes --execute --receipt-out` writes value hash and doctor confirmation evidence |
| KV derived refresh receipt verifier | Implemented | v0.96 | `operations`, `durability`, `audit` | `repair kv-cache receipt verify --receipt --value-file` validates receipt structure, embedded report evidence, doctor command, and optional reviewed value hash |
| KV derived refresh receipt doctor lane | Implemented | v0.99 | `operations`, `durability`, `audit` | `contextdb doctor --kv-refresh-receipt --kv-refresh-value-file` validates derived KV refresh receipts inside the combined health report |
| Snapshot backup/restore | Implemented | v0.17 | `operations`, `backup`, `durability` | Go client and `contextdb snapshot export/import` provide NDJSON backup, seeded export filters, namespace override, and import dry-run validation |
| Snapshot restore reports | Implemented | v0.18 | `operations`, `backup`, `inspectability` | Go client report helpers and `contextdb snapshot import --report` summarize lines, records, vectors, and namespace overrides |
| Snapshot backup marker | Implemented | v0.19 | `operations`, `backup`, `durability` | `contextdb snapshot export --backup-marker` writes a doctor-compatible marker only after export succeeds |
| Snapshot diff preview | Implemented | v0.20 | `operations`, `backup`, `inspectability` | Snapshot restore reports classify nodes as new, changed, or unchanged during dry-run and import |
| Backup runbook | Implemented | v0.21 | `operations`, `backup`, `deployment` | Deployment docs provide scheduled export, restore preview, marker, doctor, launchd, systemd, and Norn check flow |
| Backup artifact manifest | Implemented | v0.22 | `operations`, `backup`, `audit` | `contextdb snapshot export --manifest` writes a checksummed JSON sidecar with namespace, version, bytes, marker path, and record counts |
| Backup manifest verify | Implemented | v0.23 | `operations`, `backup`, `audit` | `contextdb snapshot verify --manifest --in` validates checksum, size, and record counts before restore |
| Restore rehearsal | Implemented | v0.24 | `operations`, `backup`, `audit` | `contextdb snapshot rehearse --manifest --in --namespace` verifies the artifact and returns dry-run restore counts in one preflight report |
| Restore promotion checklist | Implemented | v0.25 | `operations`, `backup`, `audit` | Rehearsal reports include timestamp, target namespace, and a shell-quoted recommended import command |
| Restore promotion receipt | Implemented | v0.26 | `operations`, `backup`, `audit` | `contextdb snapshot import --promotion-report --promotion-note` writes a JSON receipt with import counts after successful promotion |
| Promotion receipt verify | Implemented | v0.27 | `operations`, `backup`, `audit` | `contextdb snapshot receipt verify --promotion-report --manifest` compares promotion receipts with artifact manifests |
| Backup lifecycle bundle | Implemented | v0.28 | `operations`, `backup`, `audit` | Backup runbook provides a guarded script for export, verify, rehearse, doctor, optional promotion, receipt verification, and lifecycle summary |
| Lifecycle summary verify | Implemented | v0.29 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle verify --summary --report` checks a lifecycle summary and its referenced backup, manifest, rehearsal, promotion, and receipt-check artifacts |
| Lifecycle retention report | Implemented | v0.30 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle retention --dir --keep --report` groups lifecycle bundles and marks newest artifacts to keep versus older pruneable bundles without deleting files |
| Lifecycle delete plan | Implemented | v0.31 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle retention --emit-delete-script` prints reviewed `rm -- ...` commands for pruneable artifacts without deleting files |
| Lifecycle manifest index | Implemented | v0.32 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle index --dir --out --report` writes a compact JSON catalog with bundle retention decisions, artifact sizes, and hashes |
| Lifecycle index verify | Implemented | v0.33 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle index verify --in --report` re-checks indexed artifact existence, byte sizes, and SHA-256 hashes |
| Lifecycle index diff | Implemented | v0.34 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle index diff --old --new --report` compares backup catalogs for bundle membership, retention decision, and artifact hash changes |
| Lifecycle index publish | Implemented | v0.36 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle index publish --in --dry-run --report` validates backup catalog metadata for ops publication without uploading backup contents |
| Lifecycle index publish drift | Implemented | v0.47 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle index publish drift --in --published-url --report` compares local and published backup catalog metadata |
| Lifecycle index publish freshness | Implemented | v0.52 | `operations`, `backup`, `audit` | `contextdb snapshot lifecycle index publish freshness --published-url --max-age --report` checks published catalog age |
| Release health page | Implemented | v0.11.2 | `operations`, `release`, `durability` | Docs page records unit, docs-build, ranking, durability, API contract, and race/soak release gates |
| Durability and ranking tests | Implemented | v0.4 | `durability`, `ranking` | Badger restart test, ranking golden fixtures, representative corpus ranking coverage, gRPC contract test, REST failure-path coverage |
| Ranking eval snapshots | Implemented | v0.48 | `ranking`, `release`, `inspectability` | `contextdb eval ranking --out --report` emits top-k, MRR, expected rank, and score breakdowns for the representative corpus |
| Ranking eval Markdown recap | Implemented | v0.55 | `ranking`, `release`, `inspectability` | `contextdb eval ranking --markdown` and `--markdown-out` emit pass/fail totals, MRR, failures, top results, and score breakdowns |
| Ranking eval historical diff | Implemented | v0.56 | `ranking`, `release`, `inspectability` | `contextdb eval ranking --compare previous.json --diff-markdown` emits MRR, pass, rank, and top-score movements between snapshots |
| Ranking eval baseline policy | Implemented | v0.61 | `ranking`, `release`, `inspectability` | `contextdb eval ranking --baseline-dir` writes versioned baseline artifacts and `--compare-baseline-dir` resolves the latest previous baseline |
| Ranking eval baseline retention | Implemented | v0.64 | `ranking`, `release`, `inspectability` | `contextdb eval ranking --baseline-retention-dir` reports current, retained, and pruneable baseline artifacts |
| Ranking eval baseline delete script | Implemented | v0.68 | `ranking`, `release`, `inspectability` | `contextdb eval ranking --baseline-retention-dir --emit-delete-script` emits a reviewed shell deletion plan for existing pruneable baseline artifacts without deleting files |
| Ranking baseline retention cookbook | Implemented | v0.72 | `ranking`, `release`, `inspectability` | Deployment docs provide keep-count, CI artifact, review, and delete-script recipes for ranking baseline history |
| Ranking baseline artifact manifest | Implemented | v0.76 | `ranking`, `release`, `audit` | `contextdb eval ranking --baseline-manifest-out` writes artifact inventory JSON with bytes and SHA-256 hashes |
| Ranking baseline artifact manifest verify | Implemented | v0.80 | `ranking`, `release`, `audit` | `contextdb eval ranking baseline manifest verify --manifest` verifies inventory paths, byte sizes, and SHA-256 hashes |
| Ranking baseline manifest verification Markdown | Implemented | v0.84 | `ranking`, `release`, `audit` | `contextdb eval ranking baseline manifest verify --markdown-out` writes compact verification recaps |
| Ranking manifest failure annotations | Implemented | v0.85 | `ranking`, `release`, `ci` | `contextdb eval ranking baseline manifest verify --annotations` and `--annotations-out` emit CI annotation lines for failed artifacts |
| Ranking manifest annotation workflow docs | Implemented | v0.86 | `ranking`, `release`, `ci` | Deployment docs include a GitHub Actions recipe for JSON, Markdown, and annotation artifacts |
| Ranking baseline verification bundle | Implemented | v0.87 | `ranking`, `release`, `audit` | `contextdb eval ranking baseline manifest verify --bundle-dir DIR` writes JSON, Markdown, and annotation files with stable names |
| Ranking bundle index verifier | Implemented | v0.90 | `ranking`, `release`, `audit` | `contextdb eval ranking baseline manifest bundle verify --index PATH` re-hashes bundle artifacts and checks JSON report status consistency |
| Mini/Norn deployment notes | Implemented | v0.3 | `operations` | Internal live deployment discovery and health-check docs |
| Norn registration helper | Implemented | v0.14 | `operations`, `deployment` | `contextdb norn manifest` and `contextdb norn validate` generate and check service entries |
| Norn live drift check | Implemented | v0.16 | `operations`, `deployment` | `contextdb norn drift` compares expected local service metadata with the live Norn manifest and reports field differences |
| Norn manifest publish | Implemented | v0.35 | `operations`, `deployment` | `contextdb norn publish --dry-run --report` validates a publish plan by default, with `--execute --publish-url` for explicit HTTP registration |
| Review escalation rules | Implemented | v0.37 | `review`, `operations` | Review queues can add escalation metadata for aged assigned or snoozed tasks and high-priority source anomaly items |
| Review escalation digest | Implemented | v0.38 | `review`, `operations` | Review escalation digests group escalated tasks by owner, source, item type, and escalation level |
| Review escalation digest export | Implemented | v0.39 | `review`, `operations` | Review escalation digest snapshots can be recorded and listed for durable handoffs |
| Review handoff feed | Implemented | v0.40 | `review`, `operations` | Saved escalation digest snapshots can be polled by owner and escalation level |
| Review handoff webhook plans | Implemented | v0.41 | `review`, `operations` | Signed dry-run webhook delivery plans expose payload, headers, and retry metadata without sending requests |
| Review handoff webhook execution | Implemented | v0.42 | `review`, `operations` | Explicit webhook delivery sends synchronous POSTs with timeout, status, body, and error capture |
| Review handoff delivery receipts | Implemented | v0.43 | `review`, `operations`, `audit` | Executed handoff webhooks append durable receipts with target URL, status, payload hash, response hash, and errors |
| Review handoff retry candidates | Implemented | v0.44 | `review`, `operations`, `audit` | Failed handoff receipts are grouped by digest and target URL for retry review without sending retries |
| Review handoff retry execution | Implemented | v0.45 | `review`, `operations`, `audit` | Failed handoff candidates can be resent explicitly by digest event ID and target URL with a new receipt recorded |
| Review handoff retry backoff | Implemented | v0.46 | `review`, `operations`, `audit` | Failed handoff candidates include read-only retry pacing recommendations based on attempt history |
| Review handoff retry fatigue | Implemented | v0.51 | `review`, `operations`, `audit` | Retry recommendations are grouped by target endpoint with candidate, attempt, readiness, and status-family counts |
| Review handoff retry fatigue Markdown | Implemented | v0.53 | `review`, `operations`, `handoff` | REST `retry-fatigue?format=markdown` and Go helper render endpoint fatigue summaries for incident notes |
| Review handoff retry fatigue owner groups | Implemented | v0.58 | `review`, `operations`, `handoff` | Retry fatigue summaries include owner and escalation-level counts in JSON, GraphQL, and Markdown |
| Review handoff retry fatigue filters | Implemented | v0.60 | `review`, `operations`, `handoff` | REST, GraphQL, and Go SDK can filter retry fatigue by owner and escalation level |
| Review handoff retry fatigue cookbook | Implemented | v0.65 | `review`, `operations`, `handoff` | Deployment docs provide owner and escalation-lane recipes for retry fatigue handoffs |
| Review handoff retry fatigue presets | Implemented | v0.69 | `review`, `operations`, `handoff` | REST, GraphQL, and Go SDK support stable preset names for repeated owner and escalation lanes |
| Retry fatigue preset discovery docs | Implemented | v0.73 | `review`, `operations`, `handoff` | Deployment docs provide preset names, expanded filters, and intended handoff audiences for dashboards |
| Retry fatigue preset examples | Implemented | v0.77 | `review`, `operations`, `handoff` | Preset metadata includes copyable REST query and GraphQL argument snippets |
| Retry fatigue preset docs drift test | Implemented | v0.81 | `review`, `operations`, `test` | SDK tests verify cookbook preset rows match `ReviewHandoffRetryFatiguePresets()` metadata |
| Retry fatigue preset schema fixture | Implemented | v0.94 | `review`, `operations`, `test` | A shared JSON schema fixture guards SDK and REST preset payload fields, order, and required examples |
| Retry fatigue preset schema publication | Implemented | v0.97 | `review`, `operations`, `docs` | The preset JSON schema is published at `/schemas/retry-fatigue-presets.schema.json` and checked against the embedded fixture |
| Published schema catalog | Implemented | v0.100 | `review`, `operations`, `docs` | `/schemas/index.json` lists stable docs schemas with URLs, owners, feature names, status, and release provenance |
| Admin/debug UI | Implemented | v0.88 | `inspectability`, `operations` | Observe port serves `/admin/` with runtime stats and a belief debugger backed by `/admin/api/belief` |
| Admin debugger search | Implemented | v0.89 | `inspectability`, `operations` | `/admin/api/search` finds recent valid graph nodes by text, source, label, or ID and the UI can open them in the debugger |
| Admin metrics dashboard | Implemented | v0.91 | `inspectability`, `operations`, `metrics` | `/admin/` surfaces health signals, ingest/retrieval rates, latency, and raw `/admin/api/metrics` JSON |
| Svelte admin shell | Implemented | v0.92 | `inspectability`, `operations`, `ui` | `/admin/` is served from an embedded Svelte app bundle while preserving metrics, search, and belief debugger APIs |
| Debugger explain-rank compare | Implemented | v0.93 | `inspectability`, `ranking`, `ui` | `/admin/api/explain-rank` and the Svelte dashboard compare two nodes with rank summary and factor deltas |

## Next Candidates

1. Doctor backup receipt closure bundle CLI for generating artifact manifests.
2. KV receipt verification fixture bundle for docs consumers and CI examples.
3. Schema catalog docs badge or CI annotation for published schema drift.
