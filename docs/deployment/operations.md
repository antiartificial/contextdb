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
  contextdb snapshot import --namespace my-app --in my-app.contextdb.ndjson --report
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

For a complete scheduled workflow that pairs export, restore preview, marker checks, launchd/systemd timers, and Norn drift checks, see the [Backup Runbook](backup-runbook).

Future doctor slices should add deeper store/index consistency checks.
