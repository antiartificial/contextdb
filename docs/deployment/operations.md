---
title: Operations
---

# Operations

## Doctor

`contextdb doctor` checks a live REST deployment and prints a JSON report:

```bash
contextdb doctor --url http://localhost:7701
```

The default doctor checks are intentionally non-mutating:

| Check | Verifies |
|:------|:---------|
| `ping` | `/v1/ping` responds with `status: ok` |
| `version` | `/v1/version` returns release and API metadata |
| `features` | `/v1/features` advertises feature introspection |
| `migrations` | `/v1/migrations` returns embedded Postgres migrations |

Exit codes:

| Code | Meaning |
|:-----|:--------|
| `0` | All checks passed |
| `1` | One or more checks failed |
| `2` | The doctor command itself could not run or encode its report |

Use `CONTEXTDB_REST_URL` to set the target without passing `--url`:

```bash
CONTEXTDB_REST_URL=http://contextdb.local:7701 contextdb doctor
```

### Sample Write Probe

Add `--sample-write` to verify the live write path, vector retrieval path, and index visibility:

```bash
contextdb doctor --url http://localhost:7701 --sample-write
```

The probe writes a deduplicated `DoctorProbe` node and retrieves it by vector. It writes to `_doctor` by default; use `--sample-namespace` to choose another namespace:

```bash
contextdb doctor --sample-write --sample-namespace ops-checks
```

For backup readiness, point doctor at a marker file written by your backup job. The check is opt-in and fails when the marker is missing, is a directory, or is older than `--max-backup-age`:

```bash
contextdb doctor --backup-marker /var/lib/contextdb/.last-backup --max-backup-age 24h
```

The JSON report includes a `backup_readiness` check with the observed marker age.

If your lifecycle index metadata is published for Norn or another ops surface, add a published catalog freshness check to the same doctor report:

```bash
contextdb doctor \
  --published-backup-url https://ops.example/contextdb/lifecycle-index.json \
  --max-published-backup-age 24h
```

The check fetches the published metadata, validates its `generated_at`, and reports `published_backup_freshness` without downloading backup contents.

### Store Consistency

Add `--store-consistency` when doctor runs on the same host and environment as the local contextdb data directory:

```bash
CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb doctor --store-consistency --store-namespace my-app --store-sample 100
```

The check samples valid graph nodes, verifies content fingerprint lookup for fingerprinted nodes, and uses vector search to confirm vector-bearing graph nodes are visible in the vector index. Missing vector hits are reported as rebuild candidates. The check is read-only and opt-in.

Add one or more `--kv-key` flags to sample expected hot cache keys during the same local doctor run:

```bash
CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb doctor --kv-key context:my-app:active --kv-key context:my-app:summary
```

Missing keys are reported as refresh candidates in a `kv_consistency` check. The check only reads the configured keys and does not refresh or mutate the cache.

For derived KV values, add `--kv-derived-key` and optionally tune `--max-kv-derived-age`:

```bash
CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb doctor --kv-derived-key context:prod:support:recent-nodes --max-kv-derived-age 2h
```

The `kv_derived_freshness` check reads the cached JSON metadata, validates the `kind` and `generated_at` fields, and reports stale, missing, or malformed derived values without rewriting the cache.

Use `contextdb repair kv-cache` after reviewing missing hot keys and choosing the exact cache value to restore. The command is dry-run by default and only writes with `--execute`:

```bash
CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb repair kv-cache --key context:my-app:active --value-file ./active-context.json --report

CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb repair kv-cache --key context:my-app:active --value-file ./active-context.json --execute --report
```

The repair report lists present keys, missing keys, refresh candidates, skipped keys, and written keys. Existing keys are skipped unless `--overwrite` is set.

For graph-derived session context values, use the [KV derivation recipes](/deployment/kv-derivation-recipes) to choose stable hot-key names, labels, and review steps before executing a refresh.

Use `contextdb repair vector-index` after reviewing candidates and choosing a maintenance window. The command is dry-run by default and only mutates the vector index with `--execute`:

```bash
CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb repair vector-index --namespace my-app --sample 100 --report

CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb repair vector-index --namespace my-app --sample 100 --execute --report
```

The repair report lists sampled vector nodes, candidate IDs, and reindexed IDs. It rebuilds vector entries from the graph node vector, model ID, and text metadata without changing graph nodes.

## Snapshot Backup And Restore

Use `contextdb snapshot export` to write an NDJSON namespace backup:

```bash
CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb snapshot export \
    --namespace my-app \
    --out my-app.contextdb.ndjson \
    --backup-marker /var/lib/contextdb/.last-backup \
    --manifest my-app.contextdb.manifest.json
```

Use seed IDs for a filtered subgraph export:

```bash
contextdb snapshot export \
  --namespace my-app \
  --seeds 550e8400-e29b-41d4-a716-446655440000 \
  --max-depth 3 \
  --out claim-subgraph.ndjson
```

Validate a backup without writing:

```bash
contextdb snapshot import --namespace restore-preview --in my-app.contextdb.ndjson --dry-run --report
```

Restore into a namespace:

```bash
CONTEXTDB_DATA_DIR=/var/lib/contextdb \
  contextdb snapshot import \
    --namespace my-app \
    --in my-app.contextdb.ndjson \
    --report \
    --promotion-note "promoted after rehearsal" \
    --promotion-report my-app.contextdb.promotion.json
```

The report includes processed line, node, edge, source, vector, namespace override, and node diff counts (`new_nodes`, `changed_nodes`, `unchanged_nodes`). Imports override the snapshot record namespace with the `--namespace` value, so the same backup can be restored into a preview namespace before replacing production data.

The export marker and optional artifact manifest are written only after the snapshot stream completes successfully. The manifest records the namespace, contextdb version, backup filename, byte size, SHA-256 checksum, marker path, and node/edge/source record counts. Point `contextdb doctor --backup-marker` at the same marker file to include backup freshness in readiness checks.

Verify a backup against its manifest before restore:

```bash
contextdb snapshot verify \
  --manifest my-app.contextdb.manifest.json \
  --in my-app.contextdb.ndjson \
  --report
```

The verify command recomputes the backup byte size, SHA-256 checksum, and node/edge/source record counts. It exits non-zero when the artifact does not match the manifest.

Rehearse a restore when you want artifact verification and dry-run import counts in one report:

```bash
contextdb snapshot rehearse \
  --manifest my-app.contextdb.manifest.json \
  --in my-app.contextdb.ndjson \
  --namespace my-app-restore-preview \
  --report
```

The rehearsal report includes the verification result plus the same dry-run restore counts returned by `contextdb snapshot import --dry-run --report`. It also records `rehearsed_at`, `target_namespace`, and a shell-quoted `recommended_import_command` for the promotion step.

For real imports, `--promotion-report` writes a JSON receipt only after the import succeeds. The receipt includes `promoted_at`, `namespace`, `backup_file`, `contextdb_version`, the optional `promotion_note`, and the full import report.

Verify a promotion receipt against the original manifest:

```bash
contextdb snapshot receipt verify \
  --promotion-report my-app.contextdb.promotion.json \
  --manifest my-app.contextdb.manifest.json \
  --report
```

Receipt verification compares schema versions, backup identity, import namespace consistency, and node/edge/source record counts.

For a complete scheduled workflow that pairs export, restore preview, marker checks, launchd/systemd timers, and Norn drift checks, see the [Backup Runbook](backup-runbook).

Future doctor slices should add deeper store/index consistency checks.
