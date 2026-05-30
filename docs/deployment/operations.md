---
title: Operations
---

# Operations

## Doctor

`contextdb doctor` checks a live REST deployment and prints a JSON report:

```bash
contextdb doctor --url http://localhost:7701
```

The first v0.4.0 doctor checks are intentionally non-mutating:

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

Future doctor slices should add explicit opt-in write/retrieve probes, index consistency checks, and backup readiness checks.
