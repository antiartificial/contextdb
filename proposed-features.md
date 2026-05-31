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

## Completed In v0.11.1

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Representative corpus ranking tests | Implemented | `TestRepresentativeCorpusRankingGolden` covers poisoning resistance, temporal memory, procedural memory, and general RAG queries over the synthetic corpus |
| Ranking candidate-pool hardening | Implemented | Hybrid retrieval now rescans at least 50 vector candidates before final score fusion, so crowded near-match topics cannot hide stronger claims before scoring |

## Completed In v0.11.2

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Release health page | Implemented | `docs/release-health.md` lists unit, docs-build, ranking, durability, API contract, and race/soak gates by release |

## Completed In v0.12.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review workflow persistence | Implemented | Go SDK, REST, and GraphQL expose append-only review decisions; derived queue items overlay latest status, owner, decision, note, and re-check time |

## Completed In v0.13.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Source trust anomaly alerts | Implemented | Review queues emit `source_trust_anomaly` tasks for configured source credibility drops, low trust thresholds, and repeated refutations through Go SDK, REST, and GraphQL |

## Completed In v0.14.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Local Norn registration helper | Implemented | `contextdb norn manifest` generates a contextdb service entry and `contextdb norn validate` checks app, endpoint, service name, and REST port |

## Completed In v0.15.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review queue filters | Implemented | Go SDK, REST, and GraphQL review queues filter by task type, source ID, workflow status, and owner; undecided tasks match `open` |

## Completed In v0.16.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Norn live drift check | Implemented | `contextdb norn drift` compares the expected generated manifest entry with the live Norn manifest and reports field-level differences |

## Completed In v0.17.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Snapshot backup/restore command | Implemented | Go client snapshot helpers and `contextdb snapshot export/import` provide NDJSON backup, seeded subgraph export, namespace restore override, and dry-run validation |

## Completed In v0.18.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Snapshot restore report | Implemented | Go client report helpers and `contextdb snapshot import --report` summarize processed lines, nodes, edges, sources, vectors, and namespace overrides |

## Completed In v0.19.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Scheduled backup marker | Implemented | `contextdb snapshot export --backup-marker PATH` writes a doctor-compatible marker only after successful export |

## Completed In v0.20.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Snapshot diff preview | Implemented | Snapshot restore reports classify node records as new, changed, or unchanged during dry-run and import |

## Completed In v0.21.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Automated backup runbook | Implemented | Deployment docs provide a scheduled backup workflow using snapshot export, restore preview, backup markers, doctor verification, launchd/systemd timers, and Norn drift checks |

## Completed In v0.22.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup artifact manifest | Implemented | `contextdb snapshot export --manifest PATH` writes a checksummed JSON sidecar with namespace, contextdb version, backup file, byte size, marker path, and record counts |

## Completed In v0.23.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup manifest verify | Implemented | `contextdb snapshot verify --manifest PATH --in PATH` validates backup byte size, SHA-256 checksum, and node/edge/source record counts before restore |

## Completed In v0.24.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Restore rehearsal command | Implemented | `contextdb snapshot rehearse --manifest PATH --in PATH --namespace NAME` verifies the backup artifact and returns dry-run restore counts in one preflight report |

## Completed In v0.25.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Restore promotion checklist | Implemented | Rehearsal reports include `rehearsed_at`, `target_namespace`, and a shell-quoted `recommended_import_command` for the promotion step |

## Completed In v0.26.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Restore promotion audit event | Implemented | `contextdb snapshot import --promotion-report PATH --promotion-note TEXT` writes a JSON promotion receipt with import counts after a successful real import |

## Completed In v0.27.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Promotion receipt verification | Implemented | `contextdb snapshot receipt verify --promotion-report PATH --manifest PATH` compares promotion receipts with artifact manifests for backup identity, namespace consistency, and record counts |

## Completed In v0.28.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup lifecycle bundle | Implemented | Backup runbook includes a guarded lifecycle script for export, verify, rehearse, doctor backup freshness, optional promotion, receipt verification, and summary output |

## Completed In v0.29.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Lifecycle summary verifier | Implemented | `contextdb snapshot lifecycle verify --summary PATH --report` checks the lifecycle summary and its referenced backup, manifest, rehearsal, promotion, and receipt-check artifacts |

## Completed In v0.30.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup lifecycle retention report | Implemented | `contextdb snapshot lifecycle retention --dir PATH --namespace NAME --keep N --report` groups lifecycle bundles and marks newest artifacts to keep versus older pruneable bundles without deleting files |

## Completed In v0.31.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup retention delete plan | Implemented | `contextdb snapshot lifecycle retention --emit-delete-script` prints reviewed `rm -- ...` commands for existing artifacts in pruneable bundles without deleting files |

## Completed In v0.32.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup lifecycle manifest index | Implemented | `contextdb snapshot lifecycle index --dir PATH --namespace NAME --keep N --out PATH --report` writes a compact JSON catalog with bundle timestamps, retention decisions, delete-plan commands, artifact sizes, and SHA-256 hashes |

## Completed In v0.33.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup index verification | Implemented | `contextdb snapshot lifecycle index verify --in PATH --report` re-checks indexed artifact paths, byte sizes, and SHA-256 hashes |

## Completed In v0.34.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup index summary diff | Implemented | `contextdb snapshot lifecycle index diff --old PATH --new PATH --report` compares backup catalogs for bundle membership, retention decision, and artifact size or SHA-256 deltas |

## Completed In v0.35.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Norn manifest publish | Implemented | `contextdb norn publish --dry-run --report` validates a publish plan by default, with `--execute --publish-url` for explicit authenticated registration writes |

## Completed In v0.36.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup index publish to Norn | Implemented | `contextdb snapshot lifecycle index publish --in PATH --dry-run --report` validates backup catalog metadata for publication without uploading backup contents |

## Completed In v0.37.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review escalation rules | Implemented | Review queues add escalation metadata for aged assigned or due snoozed tasks and high-priority source anomaly items through Go, REST, and GraphQL |

## Completed In v0.38.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review escalation digest | Implemented | Go SDK, REST, and GraphQL expose grouped escalation summaries by owner, source, item type, and escalation level |

## Completed In v0.39.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review escalation digest export | Implemented | Go SDK, REST, and GraphQL can record and list durable escalation digest snapshots for handoffs |

## Completed In v0.40.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review handoff subscriptions | Implemented | Go SDK, REST, and GraphQL expose polling-friendly handoff feeds filtered by owner and escalation level |

## Completed In v0.41.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review handoff webhook plans | Implemented | Go SDK, REST, and GraphQL expose signed dry-run webhook delivery plans with payload hashes, optional HMAC signatures, headers, and retry metadata |

## Completed In v0.42.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review handoff webhook execution | Implemented | Go SDK, REST, and GraphQL send opt-in synchronous webhook deliveries with explicit `execute`, timeout, status, response body, and error capture |

## Completed In v0.43.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review handoff delivery receipts | Implemented | Go SDK, REST, and GraphQL expose append-only webhook delivery receipt events with target URL, status, payload hash, response hash, and error text |

## Completed In v0.44.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review handoff retry queue | Implemented | Go SDK, REST, and GraphQL expose read-only retry candidates grouped by digest event and target URL for unresolved failed deliveries |

## Completed In v0.45.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review handoff retry execution | Implemented | Go SDK, REST, and GraphQL resend one unresolved failed handoff delivery by digest event ID and target URL, require explicit `execute`, and record the retry receipt |

## Completed In v0.46.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Review handoff retry backoff policy | Implemented | Go SDK, REST, and GraphQL expose read-only retry recommendations with recommended time, delay, readiness, and reason derived from receipt history |

## Completed In v0.47.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup publish drift watch | Implemented | `contextdb snapshot lifecycle index publish drift --in PATH --published-url URL --report` compares local backup catalog metadata with the published ops payload |

## Completed In v0.48.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking eval snapshots | Implemented | `contextdb eval ranking --out PATH --report` emits JSON top-k, expected rank, reciprocal rank, MRR, and score breakdowns for the representative corpus |

## Completed In v0.49.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Store repair/index rebuild | First slice implemented | `contextdb doctor --store-consistency --store-namespace NAME` samples valid graph nodes, checks fingerprint lookup, and reports vector rebuild candidates |

## Completed In v0.50.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Store repair execution | Implemented | `contextdb repair vector-index --namespace NAME --report` lists vector rebuild candidates and `--execute` reindexes reviewed graph-node vectors |

## Completed In v0.51.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue summary | Implemented | Go SDK, REST, and GraphQL group unresolved review handoff retry pressure by target endpoint with attempts, readiness, status families, and latest failure |

## Completed In v0.52.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup publish freshness monitor | Implemented | `contextdb snapshot lifecycle index publish freshness --published-url URL --max-age 24h --report` checks published backup catalog `generated_at` age |

## Completed In v0.53.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue markdown export | Implemented | Go SDK `ReviewHandoffRetryFatigueMarkdown` and REST `retry-fatigue?format=markdown` render endpoint fatigue handoff notes |

## Completed In v0.54.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV consistency sampling | First slice implemented | `contextdb doctor --kv-key KEY` samples expected hot keys and reports missing cache refresh candidates without mutating KV |

## Completed In v0.55.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking eval Markdown recap | Implemented | `contextdb eval ranking --markdown` and `--markdown-out PATH` emit a compact release-review summary with totals, MRR, failures, top results, and score breakdowns |

## Completed In v0.56.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking eval historical diff | Implemented | `contextdb eval ranking --compare previous.json --diff-report` and `--diff-markdown` compare ranking snapshots and summarize MRR, pass, rank, and top-score movements |

## Completed In v0.57.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Backup freshness doctor integration | Implemented | `contextdb doctor --published-backup-url URL --max-published-backup-age 24h` adds a `published_backup_freshness` check to the combined doctor report |

## Completed In v0.58.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue owner grouping | Implemented | Retry fatigue summaries now include owner and escalation-level breakdowns across Go SDK, REST, GraphQL, and Markdown handoff export |

## Completed In v0.59.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV refresh plan execution | Implemented | `contextdb repair kv-cache --key KEY --value/--value-file --report` plans reviewed hot-key refreshes and `--execute` writes explicit values |

## Completed In v0.60.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue owner filter | Implemented | REST `retry-fatigue?owner=&escalation_level=`, GraphQL `reviewHandoffRetryFatigue(owner:, escalationLevel:)`, and Go SDK `ReviewHandoffRetryFatigueFiltered` scope endpoint fatigue handoffs |

## Completed In v0.61.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking diff baseline policy | Implemented | `contextdb eval ranking --baseline-dir DIR` writes versioned JSON and Markdown baselines; `--compare-baseline-dir DIR` resolves the latest previous baseline for diff reports |

## Completed In v0.62.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Doctor published backup drift | Implemented | `contextdb doctor --published-backup-index PATH --published-backup-url URL` adds a `published_backup_drift` check to the combined doctor report |

## Completed In v0.63.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV refresh typed derivations | Implemented | `contextdb repair kv-cache --derive recent-nodes --derive-namespace NAME --derive-label LABEL` builds reviewed recent-node session context values from graph data |

## Completed In v0.64.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking baseline retention report | Implemented | `contextdb eval ranking --baseline-retention-dir DIR --baseline-retention-keep N` reports current, retained, and pruneable baseline artifacts without deleting files |

## Completed In v0.65.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue escalation filter docs | Implemented | `docs/deployment/retry-fatigue-cookbook.md` documents REST, Markdown, and GraphQL owner/escalation-lane recipes |

## Completed In v0.66.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Doctor published backup repair hint | Implemented | Published backup drift reports and doctor checks include `recommended_publish_command` pointing to dry-run lifecycle index publish |

## Completed In v0.67.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV derivation recipes | Implemented | `docs/deployment/kv-derivation-recipes.md` documents stable key naming, recent-node scopes, dry-run review, and execute promotion |

## Completed In v0.68.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking baseline deletion plan | Implemented | `contextdb eval ranking --baseline-retention-dir DIR --emit-delete-script` prints reviewed `rm -- ...` commands for existing pruneable ranking baseline artifacts |

## Completed In v0.69.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue saved filter presets | Implemented | REST `preset=`, GraphQL `preset:`, Go SDK `Preset`, and response preset metadata provide stable retry fatigue lane names |

## Completed In v0.70.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Published backup repair execution guard | Implemented | `docs/deployment/published-backup-repair-guard.md` documents dry-run review, execute prechecks, token scope, and post-repair verification |

## Completed In v0.71.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV derivation freshness doctor | Implemented | `contextdb doctor --kv-derived-key KEY --max-kv-derived-age DURATION` validates derived KV `generated_at` freshness |

## Completed In v0.72.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking baseline retention cookbook | Implemented | `docs/deployment/ranking-baseline-retention-cookbook.md` documents keep-count, CI artifact, retention review, and delete-script recipes |

## Completed In v0.73.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue preset discovery docs | Implemented | `docs/deployment/retry-fatigue-cookbook.md` includes preset names, expanded filters, and intended handoff audiences |

## Completed In v0.74.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Published backup repair receipt | Implemented | `contextdb snapshot lifecycle index publish --execute --receipt-out PATH` records durable publish repair evidence |

## Completed In v0.75.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV derivation freshness repair hint | Implemented | `kv_derived_freshness` doctor failures include a dry-run `contextdb repair kv-cache --derive recent-nodes` command |

## Completed In v0.76.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking baseline artifact manifest | Implemented | `contextdb eval ranking --baseline-retention-dir DIR --baseline-manifest-out PATH` writes artifact bytes and SHA-256 inventory JSON |

## Completed In v0.77.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue preset examples endpoint | Implemented | Retry fatigue preset metadata includes `example_rest_query` and `example_graphql` snippets for dashboard filters |

## Completed In v0.78.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Published backup receipt verifier | Implemented | `contextdb snapshot lifecycle index publish receipt verify --receipt PATH --in INDEX` compares receipt payload hashes and catalog metadata with the local lifecycle index |

## Completed In v0.79.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV derivation repair execution recipe | Implemented | `docs/deployment/kv-derivation-recipes.md` documents a doctor hint, dry-run review, guarded execute, and doctor confirmation flow for stale derived KV refreshes |

## Completed In v0.80.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking baseline manifest verifier | Implemented | `contextdb eval ranking baseline manifest verify --manifest PATH` verifies artifact inventory paths, byte sizes, and SHA-256 hashes |

## Completed In v0.81.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Retry fatigue preset example docs test | Implemented | `TestRetryFatigueCookbookPresetReferenceMatchesSDK` verifies cookbook preset rows match `ReviewHandoffRetryFatiguePresets()` metadata |

## Completed In v0.82.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Published backup receipt verification doctor | Implemented | `contextdb doctor --published-backup-receipt PATH --published-backup-index PATH` verifies catalog repair receipts in the combined doctor report |

## Completed In v0.83.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| KV derived refresh receipt | Implemented | `contextdb repair kv-cache --derive recent-nodes --execute --receipt-out PATH` writes a receipt with value hash, executed report, and doctor confirmation command |

## Completed In v0.84.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking baseline manifest summary export | Implemented | `contextdb eval ranking baseline manifest verify --markdown` and `--markdown-out PATH` emit compact Markdown verification recaps for CI and release review |

## Completed In v0.85.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking manifest failure annotations | Implemented | `contextdb eval ranking baseline manifest verify --annotations` and `--annotations-out PATH` emit CI annotation lines for failed manifest artifacts |

## Completed In v0.86.0

| Feature | Status | Evidence |
|:--------|:-------|:---------|
| Ranking manifest annotation fixture docs | Implemented | `docs/deployment/ranking-baseline-retention-cookbook.md` includes a GitHub Actions recipe that captures JSON, Markdown, and annotation artifacts together |

## Product And Inspection

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| Belief debugger UI | Makes nodes, score breakdowns, evidence, contradictions, source trust, and history visible in one place | Back it with the existing GraphQL surface |
| Ranking evaluation dashboard | Tracks query sets, expected nodes, recall@k, MRR, and score deltas across releases | Snapshot foundation completed in v0.48.0; next step is dashboard/UI |
| Explain-rank endpoint | Answers "why did this node rank above that one?" | Completed in v0.8.0; graph support-chain evidence completed in v0.11.0; next step is UI integration |
| Feature/version introspection | Lets clients ask which APIs and migrations are available | Completed in v0.4.0; keep expanding feature metadata as APIs mature |
| Local Norn registration helper | Reduces drift between live services and docs | Completed in v0.14.0; live drift check completed in v0.16.0; next step is optionally posting to Norn when an authenticated API is available |

## Feedback And Epistemics

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| Feedback event log | Makes validate/refute/useful/stale auditable as explicit events | Completed in v0.5.0; next step is source trust timeline views |
| Claim review queue | Turns contradictions, low confidence, and stale claims into operator tasks | Completed in v0.7.0; durable workflow decisions completed in v0.12.0 |
| Source trust timeline | Shows how source credibility changed over time | Completed in v0.6.0; anomaly review tasks completed in v0.13.0; next step is richer timeline visualization in the debugger UI |
| Knowledge acquisition planner | Converts knowledge gaps into suggested crawl/search/research tasks | Completed in v0.9.0; next step is connector-specific acquisition execution |
| Review workflow persistence | Tracks review status, owners, decisions, and re-check schedules | Completed in v0.12.0; reviewer filters completed in v0.15.0; next step is escalation rules |

## Durability And Operations

| Feature | Why it matters | Notes |
|:--------|:---------------|:------|
| `contextdb doctor` | One command to verify stores, migrations, indexes, health, and sample writes | Non-mutating checks completed in v0.4.0; opt-in sample write/retrieve probe completed in v0.4.1; store consistency sampling completed in v0.49.0 |
| Release health page | Makes release confidence visible | Completed in v0.11.2; next step is generating gate status from CI artifacts |
| Backup/restore command | Productizes snapshot import/export | Completed in v0.17.0 with Go client helpers, CLI export/import, seeded filters, namespace restore override, and dry-run validation |
| Automated backup runbook | Makes backups repeatable for live deployments | Completed in v0.21.0 with scheduled export, restore preview, backup marker, doctor, launchd/systemd, and Norn pairing docs |
| Backup artifact manifest | Makes copied backup files auditable | Completed in v0.22.0 with `contextdb snapshot export --manifest` checksummed JSON sidecars |
| Backup manifest verify | Catches mismatched or truncated artifacts before restore | Completed in v0.23.0 with `contextdb snapshot verify --manifest --in` |
| Restore rehearsal command | Collapses verify and dry-run restore into one operator preflight | Completed in v0.24.0 with `contextdb snapshot rehearse --manifest --in --namespace` |
| Restore promotion checklist | Makes the post-rehearsal import step explicit | Completed in v0.25.0 with rehearsal timestamp, target namespace, and recommended import command fields |
| Restore promotion audit event | Leaves a receipt for actual restore promotions | Completed in v0.26.0 with `contextdb snapshot import --promotion-report --promotion-note` |
| Promotion receipt verification | Confirms promotion receipts match exported artifacts | Completed in v0.27.0 with `contextdb snapshot receipt verify --promotion-report --manifest` |
| Backup lifecycle bundle | Gives operators one copyable backup-to-promotion workflow | Completed in v0.28.0 with guarded lifecycle script and summary output |
| Lifecycle summary verifier | Checks the end-to-end artifact bundle after lifecycle scripts run | Completed in v0.29.0 with `contextdb snapshot lifecycle verify --summary --report` |
| Backup lifecycle retention report | Helps operators decide which verified bundles can be pruned | Completed in v0.30.0 with dry-run retention reports grouped by lifecycle summary |
| Backup retention delete plan | Converts retention review into shell commands without deleting files | Completed in v0.31.0 with `contextdb snapshot lifecycle retention --emit-delete-script` |
| Backup lifecycle manifest index | Gives operators one portable catalog of lifecycle bundles | Completed in v0.32.0 with `contextdb snapshot lifecycle index --dir --out --report` |
| Backup index verification | Proves a saved lifecycle catalog still matches files on disk | Completed in v0.33.0 with `contextdb snapshot lifecycle index verify --in --report` |
| Backup index summary diff | Shows whether two saved lifecycle catalogs still agree across runs or hosts | Completed in v0.34.0 with `contextdb snapshot lifecycle index diff --old --new --report` |
| Norn manifest publish | Lets operators validate and then publish the generated service entry | Completed in v0.35.0 with dry-run-first `contextdb norn publish` |
| Backup index publish to Norn | Shares backup catalog state with ops tooling without moving backup contents | Completed in v0.36.0 with dry-run-first lifecycle index metadata publishing |
| Backup publish drift watch | Detects whether published backup catalog metadata still matches the latest local lifecycle index | Completed in v0.47.0 with `contextdb snapshot lifecycle index publish drift` |
| Backup publish freshness monitor | Detects stale published backup catalog metadata | Completed in v0.52.0 with read-only `contextdb snapshot lifecycle index publish freshness` |
| Review escalation rules | Highlights assigned, due snoozed, and high-priority source anomaly tasks that have aged past thresholds | Completed in v0.37.0 with review queue escalation metadata |
| Review escalation digest | Summarizes escalated review work for dashboards and handoffs | Completed in v0.38.0 with grouped digest APIs |
| Review escalation digest export | Preserves escalation handoff snapshots for later audit | Completed in v0.39.0 with durable digest events |
| Review handoff subscriptions | Lets owners poll saved escalation handoff snapshots by severity | Completed in v0.40.0 with owner/level filtered handoff feeds |
| Review handoff webhooks | Lets teams validate push-style handoff delivery before enabling outbound sends | Completed in v0.41.0 with signed dry-run webhook plans |
| Review handoff webhook execution | Lets teams send push-style handoff delivery intentionally | Completed in v0.42.0 with explicit synchronous delivery and response capture |
| Review handoff delivery receipts | Makes executed handoff delivery auditable | Completed in v0.43.0 with append-only receipt events and list APIs |
| Review handoff retry queue | Helps operators identify failed handoff deliveries that still need action | Completed in v0.44.0 with read-only retry candidates |
| Review handoff retry execution | Lets operators resend a reviewed failed handoff without introducing automatic background retries | Completed in v0.45.0 with explicit digest/target retry execution and receipt recording |
| Review handoff retry backoff policy | Helps operators pace repeated retries without adding background scheduling | Completed in v0.46.0 with read-only recommendations from receipt history |
| Review handoff retry fatigue | Helps operators spot repeatedly failing webhook endpoints | Completed in v0.51.0 with endpoint-level retry pressure summaries |
| Review handoff retry fatigue Markdown | Helps incident handoffs include retry context without JSON tooling | Completed in v0.53.0 with Go SDK and REST Markdown export |
| Store repair/index rebuild | Helps recover from vector index or KV drift | Doctor consistency slice completed in v0.49.0; dry-run-first vector repair execution completed in v0.50.0; KV hot-key sampling completed in v0.54.0; KV refresh execution remains |
| Soak/race test lane | Catches concurrency and long-running drift | Run `go test -race ./...` plus concurrent writers/readers/feedback loops |

## Test Investments

Priority additions:

1. Restart durability suite for Badger-backed embedded mode. Implemented for the core write/feedback/dedup/retrieve restart path in v0.4.0.
2. Docker-backed Postgres integration suite for migrations, fingerprint indexes, feedback, and vector retrieval.
3. Ranking golden tests for namespace modes and representative corpora. Implemented for belief-system and agent-memory presets in v0.4.0; representative corpus coverage landed in v0.11.1.
4. API contract parity tests across Go SDK, REST, gRPC, and GraphQL. Expanded in v0.4.0 with gRPC public contract and REST failure-path coverage.
5. Failure injection for unavailable vector stores, graph stores, embedders, and malformed API requests.
6. Long-running race/soak tests for concurrent writes, reads, feedback, dedup, and compaction.

## Versioning Approach

The current docs should stay latest-first, with release recap pages and feature tags. Full multi-version docs become worthwhile once there are multiple supported release lines with incompatible APIs.

## Likely Next Features

| Feature | Why it belongs next | First useful slice |
|:--------|:--------------------|:-------------------|
| Belief debugger UI | GraphQL plus introspection gives a stable product surface for an inspection tool | Read-only local UI for search results, explain-rank comparisons, sources, edges, and narrative reports |
| Release health page | The release process now has concrete test categories to report | Completed in v0.11.2; next step is generated CI-backed status |
| Explain-rank graph evidence | The first explain-rank slice covers score deltas; graph-aware evidence makes explanations deeper | Support-chain evidence completed in v0.11.0; source trust context and contradiction path summaries remain useful next steps |
| Doctor backup readiness | The doctor command now has live metadata and write/read checks; backup checks make it more operationally complete | Completed in v0.10.0; deeper store/index consistency checks remain |
| Review workflow persistence | The derived queue now exists; operators need durable triage state around it | Completed in v0.12.0; next step is reviewer filters and escalation rules |
| Source trust anomaly alerts | Trust timelines now exist; the next step is detecting suspicious credibility drops or repeated refutations | Completed in v0.13.0; next step is reviewer-facing anomaly filters |
| Acquisition execution connectors | Planner tasks now exist; the next step is executing them through configured crawlers/search tools | Add connector hooks and dry-run previews for source-constrained acquisition tasks |
| Postgres integration harness | Standard mode needs the same confidence now covered for Badger restarts | Docker-backed test for migrations, fingerprint dedup, feedback, and vector retrieval |

## Fresh Brainstorm After v0.11.1

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Ranking eval snapshots | Ranking is now protected by corpus tests, but release-to-release score movement still needs visibility | Persist a JSON report with query ID, top result, reciprocal rank, and score breakdown for every corpus query |
| Candidate-pool telemetry | Wider rescoring improves quality, but operators should know when candidate pools are saturated | Add retrieval stats for vector candidates considered, fused candidates, and final top-k by namespace |
| Corpus authoring guide | Representative tests will age better if adding a scenario is low-friction | Document how to add fixtures, labelled queries, and expected rank cutoffs |
| Review workflow persistence | Derived review tasks are useful, but operators need durable decisions around them | Completed in v0.12.0; next step is reviewer filters and escalation rules |
| Trust anomaly review tasks | Source timelines exist, and sudden credibility drops should become actionable | Completed in v0.13.0; next step is anomaly filters and escalation rules |
| Release health page | The project now has meaningful release gates to summarize | Completed in v0.11.2; next step is generated CI-backed status by release |

## Fresh Brainstorm After v0.11.2

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| CI-backed release health | The health page is useful, but hand-written status can drift | Generate release-health data from test commands, GitHub Actions, or release artifacts |
| Ranking eval snapshots | Corpus tests protect expected ordering; snapshots would explain score movement | Emit JSON and markdown reports with top-k, MRR, and score breakdowns for each corpus query |
| Candidate-pool telemetry | Wider candidate pools improve quality but should be observable | Add retrieval counters for vector candidates fetched, fused candidates scored, and final top-k size |
| Review workflow persistence | Derived review tasks now have durable triage state | Add reviewer filters, escalation rules, and review aging metrics |
| Trust anomaly review tasks | Source trust timelines should become actionable when credibility shifts sharply | Completed in v0.13.0; next step is anomaly filters and escalation rules |

## Fresh Brainstorm After v0.12.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review filters and aging metrics | Review decisions now persist, so operators need views by owner, status, age, and snooze horizon | Queue filters completed in v0.15.0; age buckets remain a useful escalation slice |
| Review escalation rules | Snoozed or assigned tasks can silently age out without escalation | Emit high-priority review tasks when assigned items exceed an age threshold |
| Trust anomaly review tasks | Review workflow can now receive durable triage decisions | Completed in v0.13.0; next step is anomaly filters and escalation rules |
| CI-backed release health | Release health is visible but still hand-maintained | Generate release-health rows from verified command artifacts |
| Ranking eval snapshots | Ranking tests protect expected results but do not expose score drift | Emit JSON reports for top-k, reciprocal rank, and score breakdowns per corpus query |

## Fresh Brainstorm After v0.13.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Source anomaly filters | Source anomaly tasks now exist, but operators need focused views | Completed in v0.15.0 with review queue filters for type, source ID, status, and owner |
| Trust anomaly escalation rules | High-severity source drops should not wait in a generic queue forever | Completed in v0.37.0 with source anomaly escalation metadata |
| Source quarantine workflow | Repeated refutations often imply the source should be temporarily excluded | Add source label suggestions or a dry-run quarantine action tied to review decisions |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| CI-backed release health | Release health still relies on hand-written status rows | Generate release health from verified command outputs or GitHub Actions artifacts |

## Fresh Brainstorm After v0.14.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Norn manifest publish | Manifest generation exists, but registration may still be manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Norn live drift check | Validation catches local shape errors, not live manifest drift | Completed in v0.16.0 with `contextdb norn drift` |
| Source anomaly filters | Source anomaly tasks now exist, but operators need focused views | Completed in v0.15.0 with review queue filters for type, source ID, status, and owner |
| Backup/restore command | Operational readiness now has doctor checks and Norn helpers | Completed in v0.17.0 with snapshot export/import and dry-run validation |
| CI-backed release health | Release health still relies on hand-written status rows | Generate release health from verified command outputs or GitHub Actions artifacts |

## Fresh Brainstorm After v0.15.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Norn live drift check | Manifest generation exists, but hosted services can drift from local expectations | Completed in v0.16.0 with `contextdb norn drift` |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Backup/restore command | Operational readiness now has doctor checks and Norn helpers | Completed in v0.17.0 with snapshot export/import and dry-run validation |
| CI-backed release health | Release health still relies on hand-written status rows | Generate release health from verified command outputs or GitHub Actions artifacts |

## Fresh Brainstorm After v0.16.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Backup/restore command | Operational readiness now has doctor checks and Norn helpers | Completed in v0.17.0 with snapshot export/import and dry-run validation |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| CI-backed release health | Release health still relies on hand-written status rows | Generate release health from verified command outputs or GitHub Actions artifacts |

## Fresh Brainstorm After v0.17.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Snapshot restore report | Backup/restore now exists, but imports should summarize what changed | Completed in v0.18.0 with dry-run and import reports |
| Scheduled backup marker | Snapshot export pairs naturally with doctor backup readiness | Completed in v0.19.0 with `contextdb snapshot export --backup-marker` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |

## Fresh Brainstorm After v0.18.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Scheduled backup marker | Snapshot reports can prove an export/validation completed | Completed in v0.19.0 with export-side backup markers |
| Snapshot diff preview | Restore reports count records, but operators also need changed-vs-existing detail | Completed in v0.20.0 with new, changed, and unchanged node counts |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |

## Fresh Brainstorm After v0.19.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Snapshot diff preview | Restore reports count records, but operators also need changed-vs-existing detail | Completed in v0.20.0 with new, changed, and unchanged node counts |
| Automated backup runbook | Export markers and doctor readiness now exist | Completed in v0.21.0 with launchd, systemd, Norn, restore preview, marker, and doctor docs |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |

## Fresh Brainstorm After v0.20.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Automated backup runbook | Export, restore preview, diff counts, backup markers, and doctor checks now compose into a real backup workflow | Completed in v0.21.0 with launchd, systemd, Norn, restore preview, marker, and doctor docs |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.21.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup artifact manifest | Scheduled backups now have a runbook, but copied files need machine-readable metadata | Completed in v0.22.0 with `contextdb snapshot export --manifest` JSON sidecars |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.22.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup manifest verify | Export can write sidecars, but operators need a quick pre-restore validation command | Completed in v0.23.0 with `contextdb snapshot verify --manifest --in` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.23.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Restore rehearsal command | Verify and dry-run are separate; operators need one preflight workflow | Completed in v0.24.0 with `contextdb snapshot rehearse --manifest --in --namespace` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.24.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Restore promotion checklist | Rehearsal proves readiness, but promotion still needs a repeatable operator path | Completed in v0.25.0 with rehearsal timestamp, target namespace, and recommended import command fields |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.25.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Restore promotion audit event | Rehearsal now recommends the import command, but actual promotions are not logged as artifacts | Completed in v0.26.0 with optional promotion receipts on snapshot import |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.26.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Promotion receipt verification | Promotion receipts exist, but operators need to compare them with the rehearsal/import artifacts | Completed in v0.27.0 with `contextdb snapshot receipt verify --promotion-report --manifest` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.27.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup lifecycle bundle | Export, verify, rehearse, promote, and receipt verify now exist as separate commands | Completed in v0.28.0 with a guarded runbook script and lifecycle summary |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.28.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Lifecycle summary verifier | Lifecycle summaries now exist, but operators need a quick consistency check across every referenced artifact | Completed in v0.29.0 with `contextdb snapshot lifecycle verify --summary --report` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.29.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup lifecycle retention report | Lifecycle verification can prove a bundle is good; operators also need to know what can be pruned | Completed in v0.30.0 with `contextdb snapshot lifecycle retention --dir --keep --report` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.30.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup retention delete plan | Retention reports are dry-run; operators may eventually want a generated deletion script | Completed in v0.31.0 with `contextdb snapshot lifecycle retention --emit-delete-script` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.31.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup lifecycle manifest index | Retention reports scan a directory each time; operators may want one compact index of known bundles | Completed in v0.32.0 with `contextdb snapshot lifecycle index --dir --out --report` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.32.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup index verification | Manifest indexes now exist, but operators need to prove the catalog still matches files on disk | Completed in v0.33.0 with `contextdb snapshot lifecycle index verify --in --report` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.33.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup index summary diff | Index verification proves one catalog; operators may need to compare two backup catalogs across hosts | Completed in v0.34.0 with `contextdb snapshot lifecycle index diff --old --new --report` |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.34.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup index publish to Norn | Index diffs can prove backup state; the hosted mini could expose the current backup catalog to ops tooling | Add dry-run-first publication of latest index metadata to Norn once an authenticated write endpoint exists |
| Norn manifest publish | Drift detection can find stale registration, but publishing is still manual | Completed in v0.35.0 with dry-run-first `contextdb norn publish --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.35.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup index publish to Norn | Norn publish can now write service entries; backup catalog metadata could use the same dry-run-first shape | Completed in v0.36.0 with `contextdb snapshot lifecycle index publish --in --report` |
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.36.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review escalation rules | Filters make queues easier to focus; aging and severity should now drive escalation | Completed in v0.37.0 with Go, REST, and GraphQL escalation metadata |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.37.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review escalation digest | Escalation metadata is now available per item; operators still need a compact summary by owner, source, and severity | Completed in v0.38.0 with grouped Go, REST, and GraphQL escalation digests |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.38.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review escalation digest export | Digests are queryable, but weekly review handoffs may need durable snapshots | Completed in v0.39.0 with saved digest events and list APIs |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.39.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review handoff subscriptions | Durable digest snapshots exist, but owners still need timely delivery | Completed in v0.40.0 with polling-friendly handoff feeds filtered by owner and escalation level |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.40.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review handoff webhooks | Polling handoff feeds exist, but some owners need push delivery | Completed in v0.41.0 with signed dry-run webhook delivery plans and retry metadata |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.41.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review handoff webhook execution | Dry-run plans prove payload shape, but delivery still needs explicit execution controls | Completed in v0.42.0 with opt-in `execute`, timeout, response status/body capture, and no background retries |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.42.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review handoff delivery receipts | Webhook execution captures responses but does not persist delivery evidence | Completed in v0.43.0 with append-only delivery receipt events and list APIs |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.43.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review handoff retry queue | Receipts record delivery outcomes, but failed handoffs still need coordinated retry planning | Completed in v0.44.0 with retry candidates grouped by digest event and target URL without sending retries |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.44.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review handoff retry execution | Retry candidates identify failures, but resend still needs an explicit operator control | Completed in v0.45.0 with opt-in resend by digest event ID and target URL |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Add a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Emit JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Add a doctor check that compares graph nodes, vector entries, and KV fingerprints, then report rebuild candidates |

## Fresh Brainstorm After v0.45.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Review handoff retry backoff policy | Explicit retry exists, but repeated failures still need operator-safe pacing guidance | Completed in v0.46.0 with dry-run backoff recommendations from receipt history |
| Backup publish drift watch | Index metadata can now be published, but operators still need scheduled comparison against the live published payload | Completed in v0.47.0 with a dry-run report that fetches the published backup catalog metadata and compares it to the local lifecycle index |
| Ranking eval snapshots | Ranking changes continue to be important as review signals expand | Completed in v0.48.0 with JSON score-drift reports for the representative corpus |
| Store repair/index rebuild | Backup/restore confidence is better, but live stores still need deeper consistency checks | Vector candidate detection completed in v0.49.0; dry-run-first vector repair execution completed in v0.50.0 |

## Fresh Brainstorm After v0.49.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Store repair execution | Doctor can now identify vector rebuild candidates, but operators still need an explicit repair action | Completed in v0.50.0 with dry-run-first vector reindexing for reviewed candidates |
| Retry fatigue summary | Backoff guidance exists per failed handoff, but operators need to see repeated failures by endpoint | Group retry recommendation counts by target URL and status family |
| Backup publish freshness monitor | Drift comparison exists on demand, but operators still need freshness thresholds | Completed in v0.52.0 with a read-only check that compares published generated_at with a max age |
| Ranking eval markdown recap | JSON snapshots exist, but release reviewers need a compact human summary | Completed in v0.55.0 with Markdown recaps for MRR, pass/fail totals, failures, top results, and score breakdowns |

## Fresh Brainstorm After v0.50.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Retry fatigue summary | Backoff guidance exists per failed handoff, but operators need endpoint-level fatigue signals | Completed in v0.51.0 with grouped retry recommendation counts by target URL, status family, readiness, and last error |
| Backup publish freshness monitor | Published backup catalog drift can be checked on demand, but stale publications need age thresholds | Completed in v0.52.0 with a read-only freshness check for published `generated_at` and `--max-age` |
| KV consistency sampling | Vector repair now has a reviewed execution path, but KV drift is still only implicitly covered | Add doctor sampling for expected hot keys and a dry-run cache refresh plan |
| Ranking eval markdown recap | JSON snapshots exist, but release reviewers need a compact human summary | Completed in v0.55.0 with Markdown recaps for MRR, pass/fail totals, failures, top results, and score breakdowns |

## Fresh Brainstorm After v0.51.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Backup publish freshness monitor | Published backup catalog drift can be checked on demand, but stale publications need age thresholds | Completed in v0.52.0 with a read-only freshness check for published `generated_at` and `--max-age` |
| Retry fatigue markdown export | Endpoint fatigue summaries are useful, but handoffs often need human-readable incident notes | Emit Markdown from retry fatigue with top failing endpoint, readiness counts, and latest errors |
| KV consistency sampling | Vector repair now has a reviewed execution path, but KV drift is still only implicitly covered | Add doctor sampling for expected hot keys and a dry-run cache refresh plan |
| Ranking eval markdown recap | JSON snapshots exist, but release reviewers need a compact human summary | Completed in v0.55.0 with Markdown recaps for MRR, pass/fail totals, failures, top results, and score breakdowns |

## Fresh Brainstorm After v0.52.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Retry fatigue markdown export | Endpoint fatigue summaries are useful, but handoffs often need human-readable incident notes | Completed in v0.53.0 with Markdown from retry fatigue, readiness counts, and latest errors |
| KV consistency sampling | Vector repair now has a reviewed execution path, but KV drift is still only implicitly covered | Add doctor sampling for expected hot keys and a dry-run cache refresh plan |
| Ranking eval markdown recap | JSON snapshots exist, but release reviewers need a compact human summary | Completed in v0.55.0 with Markdown recaps for MRR, pass/fail totals, failures, top results, and score breakdowns |
| Backup freshness doctor integration | Published freshness now exists as a lifecycle command, but operators may want one combined health command | Completed in v0.57.0 with `published_backup_freshness` inside `contextdb doctor` |

## Fresh Brainstorm After v0.53.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| KV consistency sampling | Vector repair now has a reviewed execution path, but KV drift is still only implicitly covered | First slice completed in v0.54.0 with doctor sampling for expected hot keys |
| Ranking eval markdown recap | JSON snapshots exist, but release reviewers need a compact human summary | Completed in v0.55.0 with Markdown recaps for MRR, pass/fail totals, failures, top results, and score breakdowns |
| Backup freshness doctor integration | Published freshness now exists as a lifecycle command, but operators may want one combined health command | Completed in v0.57.0 with `published_backup_freshness` inside `contextdb doctor` |

## Fresh Brainstorm After v0.54.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| KV refresh plan execution | Doctor can identify missing hot keys, but operators still need a reviewed refresh workflow | Completed in v0.59.0 with dry-run-first `contextdb repair kv-cache` reviewed value writes |
| Ranking eval markdown recap | JSON snapshots exist, but release reviewers need a compact human summary | Completed in v0.55.0 with Markdown recaps for MRR, pass/fail totals, failures, top results, and score breakdowns |
| Backup freshness doctor integration | Published freshness now exists as a lifecycle command, but operators may want one combined health command | Completed in v0.57.0 with `published_backup_freshness` inside `contextdb doctor` |
| Retry fatigue owner grouping | Endpoint-level fatigue is useful, but review owners may need workload-specific summaries | Completed in v0.58.0 with owner and escalation counts in JSON, GraphQL, and Markdown |

## Fresh Brainstorm After v0.55.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| KV refresh plan execution | Doctor can identify missing hot keys, but operators still need a reviewed refresh workflow | Completed in v0.59.0 with dry-run-first `contextdb repair kv-cache` reviewed value writes |
| Ranking eval historical diff | Markdown recaps summarize a single run, but reviewers still need release-to-release score movement | Completed in v0.56.0 with JSON and Markdown diffs for MRR, pass, rank, and top-score movement |
| Backup freshness doctor integration | Published freshness now exists as a lifecycle command, but operators may want one combined health command | Completed in v0.57.0 with `published_backup_freshness` inside `contextdb doctor` |
| Retry fatigue owner grouping | Endpoint-level fatigue is useful, but review owners may need workload-specific summaries | Completed in v0.58.0 with owner and escalation counts in JSON, GraphQL, and Markdown |

## Fresh Brainstorm After v0.56.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| KV refresh plan execution | Doctor can identify missing hot keys, but operators still need a reviewed refresh workflow | Completed in v0.59.0 with dry-run-first `contextdb repair kv-cache` reviewed value writes |
| Backup freshness doctor integration | Published freshness now exists as a lifecycle command, but operators may want one combined health command | Completed in v0.57.0 with `published_backup_freshness` inside `contextdb doctor` |
| Retry fatigue owner grouping | Endpoint-level fatigue is useful, but review owners may need workload-specific summaries | Completed in v0.58.0 with owner and escalation counts in JSON, GraphQL, and Markdown |
| Ranking diff baseline policy | Snapshot diffs exist, but release workflows still need guidance on which baseline to compare | Completed in v0.61.0 with --baseline-dir and --compare-baseline-dir versioned ranking artifacts |

## Fresh Brainstorm After v0.57.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| KV refresh plan execution | Doctor can identify missing hot keys, but operators still need a reviewed refresh workflow | Completed in v0.59.0 with dry-run-first `contextdb repair kv-cache` reviewed value writes |
| Retry fatigue owner grouping | Endpoint-level fatigue is useful, but review owners may need workload-specific summaries | Completed in v0.58.0 with owner and escalation counts in JSON, GraphQL, and Markdown |
| Ranking diff baseline policy | Snapshot diffs exist, but release workflows still need guidance on which baseline to compare | Completed in v0.61.0 with --baseline-dir and --compare-baseline-dir versioned ranking artifacts |
| Doctor published backup drift | Freshness checks age, but operators may also want doctor to compare local and published catalog content | Completed in v0.62.0 with --published-backup-index in the combined doctor report |

## Fresh Brainstorm After v0.58.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| KV refresh plan execution | Doctor can identify missing hot keys, but operators still need a reviewed refresh workflow | Completed in v0.59.0 with dry-run-first `contextdb repair kv-cache` reviewed value writes |
| Ranking diff baseline policy | Snapshot diffs exist, but release workflows still need guidance on which baseline to compare | Completed in v0.61.0 with --baseline-dir and --compare-baseline-dir versioned ranking artifacts |
| Doctor published backup drift | Freshness checks age, but operators may also want doctor to compare local and published catalog content | Completed in v0.62.0 with --published-backup-index in the combined doctor report |
| Retry fatigue owner filter | Owner grouping is visible, but operators may want endpoint fatigue scoped to one owner | Completed in v0.60.0 with owner and escalation filters for REST, GraphQL, and Go SDK |

## Fresh Brainstorm After v0.59.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Ranking diff baseline policy | Snapshot diffs exist, but release workflows still need guidance on which baseline to compare | Completed in v0.61.0 with --baseline-dir and --compare-baseline-dir versioned ranking artifacts |
| Doctor published backup drift | Freshness checks age, but operators may also want doctor to compare local and published catalog content | Completed in v0.62.0 with --published-backup-index in the combined doctor report |
| Retry fatigue owner filter | Owner grouping is visible, but operators may want endpoint fatigue scoped to one owner | Completed in v0.60.0 with owner and escalation filters for REST, GraphQL, and Go SDK |
| KV refresh typed derivations | Explicit value refreshes exist, but common cache keys could be derived from graph data | Completed in v0.63.0 with --derive recent-nodes for reviewed session context values |

## Fresh Brainstorm After v0.86.0

| Feature | Why it belongs | First useful slice |
|:--------|:---------------|:-------------------|
| Ranking baseline verification bundle command | CI recipes now combine several flags, but operators may want one durable bundle path | Add a command or preset that writes JSON report, Markdown recap, and annotation file with stable names |
| Retry fatigue preset API schema fixture | Preset docs now have drift coverage, but API examples could use a small schema fixture | Add a test fixture for retry fatigue preset JSON fields across SDK and REST |
| Doctor backup receipt runbook lane | Receipt verification is in doctor, but teams may want a full incident checklist | Add a deployment recipe combining freshness, drift, receipt verify, and repair closure |
| KV derived refresh receipt verifier | Refresh receipts now exist, but incident review may need later integrity checks | Add a verifier that recomputes report/value hash and checks written-key doctor commands |
