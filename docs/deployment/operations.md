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

Future doctor slices should add deeper store/index consistency checks.
