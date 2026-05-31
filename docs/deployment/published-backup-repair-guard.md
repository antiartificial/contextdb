---
title: Published Backup Repair Guard
---

# Published Backup Repair Guard

Published backup drift reports can recommend a dry-run publish command when local backup catalog metadata differs from the metadata visible to operations dashboards. Use this guard before adding `--execute` to any recommended command.

## Start With Doctor

Run the combined doctor check first:

```bash
contextdb doctor \
  --published-backup-index "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --published-backup-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL" \
  --report
```

Only continue when the failure is limited to `published_backup_drift` and the report includes `recommended_publish_command`.

## Review The Dry Run

Run the recommended command exactly as a dry run:

```bash
contextdb snapshot lifecycle index publish \
  --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --publish-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISH_URL" \
  --report
```

Check that the dry-run report points to the expected endpoint, generated timestamp, bundle count, retention decisions, artifact count, indexed bytes, and hash coverage. The publish payload contains catalog metadata only; it does not upload NDJSON backup contents.

## Execute Only After These Checks

| Check | Why it matters |
|:------|:---------------|
| Latest local lifecycle index is verified | Avoids publishing stale or malformed catalog metadata |
| Off-host copy is complete | Keeps dashboards from advertising a backup before it is actually durable |
| Retention decisions were reviewed | Prevents accidentally replacing a published catalog with unexpected pruneable state |
| Published URL is the intended environment | Avoids writing staging metadata into production, or the reverse |
| Token is scoped to catalog metadata publish | Limits blast radius if the token is reused by automation |
| A fresh dry-run report was saved | Gives operators review evidence before the write |

When all checks pass, add `--execute --token "$NORN_TOKEN"`:

```bash
contextdb snapshot lifecycle index publish \
  --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --publish-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISH_URL" \
  --execute \
  --token "$NORN_TOKEN" \
  --receipt-out "$CONTEXTDB_BACKUP_DIR/published-backup-repair.receipt.json" \
  --report
```

`--receipt-out` is only valid with `--execute`. The receipt records the source lifecycle index path, publish endpoint, HTTP method, response status/body, publish payload hash, and the catalog metadata payload that was written. Store it beside the verified lifecycle index or with the incident record for the repair.

Verify the receipt against the local lifecycle index before closing the repair:

```bash
contextdb snapshot lifecycle index publish receipt verify \
  --receipt "$CONTEXTDB_BACKUP_DIR/published-backup-repair.receipt.json" \
  --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --report
```

The verifier compares the stored receipt payload hash, receipt payload, schema/kind, and index filename with the publish payload derived from the current local lifecycle index.

You can fold the same verification into the combined health report:

```bash
contextdb doctor \
  --published-backup-index "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --published-backup-receipt "$CONTEXTDB_BACKUP_DIR/published-backup-repair.receipt.json" \
  --report
```

## Doctor Receipt Closure Lane

Use this lane when a published backup catalog incident needs one copyable checklist from diagnosis through closeout:

| Step | Command | Closeout evidence |
|:-----|:--------|:------------------|
| 1. Diagnose freshness | `contextdb doctor --published-backup-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL" --max-published-backup-age 24h --report` | Published catalog age is visible in the combined doctor report |
| 2. Diagnose drift | `contextdb doctor --published-backup-index "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" --published-backup-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL" --report` | Drift details and `recommended_publish_command` are captured before repair |
| 3. Save dry-run publish output | `contextdb snapshot lifecycle index publish --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" --publish-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISH_URL" --report` | Operator review confirms the payload, endpoint, bundle counts, retention decisions, and indexed bytes |
| 4. Execute with a receipt | `contextdb snapshot lifecycle index publish --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" --publish-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISH_URL" --execute --token "$NORN_TOKEN" --receipt-out "$CONTEXTDB_BACKUP_DIR/published-backup-repair.receipt.json" --report` | The repair write has a durable receipt with status, response, payload hash, and catalog payload |
| 5. Verify the receipt directly | `contextdb snapshot lifecycle index publish receipt verify --receipt "$CONTEXTDB_BACKUP_DIR/published-backup-repair.receipt.json" --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" --report` | The receipt still matches the local lifecycle index payload |
| 6. Verify the receipt in doctor | `contextdb doctor --published-backup-index "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" --published-backup-receipt "$CONTEXTDB_BACKUP_DIR/published-backup-repair.receipt.json" --report` | `published_backup_receipt_verify` is green beside other health checks |
| 7. Confirm published state | `contextdb doctor --published-backup-index "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" --published-backup-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL" --max-published-backup-age 24h --report` | Freshness and drift are clear after the repair |

Attach the dry-run report, execute report, receipt, receipt verification report, and final doctor report to the incident record. The repair should not be closed from the receipt alone; close it only after the final doctor run proves the published catalog is fresh and matches the local lifecycle index.

## Confirm The Repair

After execution, rerun drift and freshness checks:

```bash
contextdb snapshot lifecycle index publish drift \
  --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --published-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL" \
  --token "$NORN_TOKEN" \
  --report

contextdb snapshot lifecycle index publish freshness \
  --published-url "$CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL" \
  --max-age 24h \
  --token "$NORN_TOKEN" \
  --report
```

The repair is complete when drift is clear, freshness is within the expected window, the receipt exists, and the published dashboard points at the intended backup catalog.
