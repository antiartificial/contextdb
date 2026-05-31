---
title: Backup Runbook
---

# Backup Runbook

This runbook wires the existing snapshot, restore preview, backup marker, doctor, and Norn checks into one repeatable backup workflow for a live contextdb namespace.

## Daily Flow

Use one timestamped backup file per namespace and a stable marker path that doctor can check:

```bash
export CONTEXTDB_DATA_DIR=/var/lib/contextdb
export CONTEXTDB_NAMESPACE=my-app
export CONTEXTDB_BACKUP_DIR=/var/backups/contextdb
export CONTEXTDB_BACKUP_MARKER=/var/lib/contextdb/.last-backup
export CONTEXTDB_REST_URL=http://localhost:7701

mkdir -p "$CONTEXTDB_BACKUP_DIR"
backup="$CONTEXTDB_BACKUP_DIR/${CONTEXTDB_NAMESPACE}-$(date -u +%Y%m%dT%H%M%SZ).ndjson"
manifest="${backup%.ndjson}.manifest.json"

contextdb snapshot export \
  --namespace "$CONTEXTDB_NAMESPACE" \
  --out "$backup" \
  --backup-marker "$CONTEXTDB_BACKUP_MARKER" \
  --manifest "$manifest"

contextdb snapshot import \
  --namespace "${CONTEXTDB_NAMESPACE}-restore-preview" \
  --in "$backup" \
  --dry-run \
  --report

contextdb doctor \
  --url "$CONTEXTDB_REST_URL" \
  --backup-marker "$CONTEXTDB_BACKUP_MARKER" \
  --max-backup-age 24h
```

The marker and artifact manifest are written only after export succeeds. The dry-run restore report should show the expected record counts and node diff counts before the backup is considered ready for retention or off-host copy.

## Promotion Check

Before restoring into the production namespace, preview into a scratch namespace and inspect the report:

```bash
contextdb snapshot import \
  --namespace "${CONTEXTDB_NAMESPACE}-restore-preview" \
  --in "$backup" \
  --dry-run \
  --report
```

`new_nodes`, `changed_nodes`, and `unchanged_nodes` show whether the backup would add, replace, or leave nodes untouched in the chosen target namespace.

## Artifact Manifest

When `--manifest` is set, export writes a JSON sidecar next to the backup:

```json
{
  "schema_version": 1,
  "namespace": "my-app",
  "backup_file": "my-app-20260530T233000Z.ndjson",
  "backup_bytes": 12345,
  "checksum_sha256": "...",
  "created_at": "2026-05-30T23:30:00Z",
  "contextdb_version": "0.35.0",
  "backup_marker": "/var/lib/contextdb/.last-backup",
  "records": {
    "lines": 42,
    "nodes": 31,
    "edges": 8,
    "sources": 3
  }
}
```

Keep the manifest with the NDJSON file when copying backups off-host. It gives scripts a stable checksum and enough counts to detect truncated or mismatched artifacts before a restore preview.

Verify the copied artifact before restore:

```bash
contextdb snapshot verify \
  --manifest "$manifest" \
  --in "$backup" \
  --report
```

If `--in` is omitted, verify looks for the manifest's `backup_file` beside the manifest. A mismatch in size, checksum, or record counts exits non-zero.

Use rehearsal when the next question is "would this restore cleanly?":

```bash
contextdb snapshot rehearse \
  --manifest "$manifest" \
  --in "$backup" \
  --namespace "${CONTEXTDB_NAMESPACE}-restore-preview" \
  --report
```

Rehearsal first verifies the artifact manifest, then runs the same dry-run import report used for restore previews. The report includes `rehearsed_at`, `target_namespace`, and a shell-quoted `recommended_import_command` that can be reviewed before promotion.

When promotion is approved, keep a receipt beside the backup artifacts:

```bash
contextdb snapshot import \
  --namespace "$CONTEXTDB_NAMESPACE" \
  --in "$backup" \
  --report \
  --promotion-note "promoted after successful rehearsal" \
  --promotion-report "${backup%.ndjson}.promotion.json"
```

The promotion receipt is written only after import succeeds and includes the note, promotion timestamp, contextdb version, and import counts.

Verify the receipt against the artifact manifest:

```bash
contextdb snapshot receipt verify \
  --promotion-report "${backup%.ndjson}.promotion.json" \
  --manifest "$manifest" \
  --report
```

Receipt verification confirms the promoted import counts still line up with the exported artifact metadata.

## Lifecycle Script

For an operator-controlled end-to-end run, wrap the commands above in one script. The script always exports, verifies, rehearses, checks doctor backup freshness, and writes a lifecycle summary. It promotes only when `PROMOTE=1` is set.

```bash
#!/usr/bin/env bash
set -euo pipefail

: "${CONTEXTDB_NAMESPACE:?set CONTEXTDB_NAMESPACE}"
: "${CONTEXTDB_BACKUP_DIR:?set CONTEXTDB_BACKUP_DIR}"
: "${CONTEXTDB_BACKUP_MARKER:?set CONTEXTDB_BACKUP_MARKER}"
: "${CONTEXTDB_REST_URL:?set CONTEXTDB_REST_URL}"

mkdir -p "$CONTEXTDB_BACKUP_DIR"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup="$CONTEXTDB_BACKUP_DIR/${CONTEXTDB_NAMESPACE}-${stamp}.ndjson"
manifest="${backup%.ndjson}.manifest.json"
rehearsal="${backup%.ndjson}.rehearsal.json"
promotion="${backup%.ndjson}.promotion.json"
receipt_check="${backup%.ndjson}.receipt-check.json"
summary="${backup%.ndjson}.lifecycle.json"
preview_namespace="${RESTORE_PREVIEW_NAMESPACE:-${CONTEXTDB_NAMESPACE}-restore-preview}"

contextdb snapshot export \
  --namespace "$CONTEXTDB_NAMESPACE" \
  --out "$backup" \
  --backup-marker "$CONTEXTDB_BACKUP_MARKER" \
  --manifest "$manifest"

contextdb snapshot verify \
  --manifest "$manifest" \
  --in "$backup" \
  --report

contextdb snapshot rehearse \
  --manifest "$manifest" \
  --in "$backup" \
  --namespace "$preview_namespace" \
  --report > "$rehearsal"

contextdb doctor \
  --url "$CONTEXTDB_REST_URL" \
  --backup-marker "$CONTEXTDB_BACKUP_MARKER" \
  --max-backup-age "${MAX_BACKUP_AGE:-24h}"

promoted=false
if [ "${PROMOTE:-0}" = "1" ]; then
  contextdb snapshot import \
    --namespace "$CONTEXTDB_NAMESPACE" \
    --in "$backup" \
    --report \
    --promotion-note "${PROMOTION_NOTE:-promoted by backup lifecycle}" \
    --promotion-report "$promotion"

  contextdb snapshot receipt verify \
    --promotion-report "$promotion" \
    --manifest "$manifest" \
    --report > "$receipt_check"
  promoted=true
fi

cat > "$summary" <<JSON
{
  "namespace": "$CONTEXTDB_NAMESPACE",
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "backup": "$backup",
  "manifest": "$manifest",
  "rehearsal": "$rehearsal",
  "promotion": "$promotion",
  "receipt_check": "$receipt_check",
  "promoted": $promoted
}
JSON
```

Use `PROMOTE=1` only after reviewing the rehearsal report. Without it, the script is a backup and preflight workflow that does not write restored records.

Verify the lifecycle summary before archiving or handing artifacts to another operator:

```bash
contextdb snapshot lifecycle verify \
  --summary "$summary" \
  --report
```

Lifecycle verification checks that the backup, manifest, and rehearsal files exist and agree. When `promoted` is true, it also requires the promotion receipt and receipt-check report, then compares the promotion receipt back to the manifest. A failed check exits non-zero and reports the missing or inconsistent artifact.

## launchd

On macOS, run the backup from a small shell script, then schedule it with launchd:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.contextdb.backup</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/contextdb-backup</string>
  </array>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key>
    <integer>3</integer>
    <key>Minute</key>
    <integer>15</integer>
  </dict>
  <key>StandardOutPath</key>
  <string>/var/log/contextdb-backup.log</string>
  <key>StandardErrorPath</key>
  <string>/var/log/contextdb-backup.err</string>
</dict>
</plist>
```

## systemd

On Linux, pair a oneshot service with a daily timer:

```ini
[Unit]
Description=contextdb namespace backup

[Service]
Type=oneshot
ExecStart=/usr/local/bin/contextdb-backup
```

```ini
[Unit]
Description=Run contextdb namespace backup daily

[Timer]
OnCalendar=*-*-* 03:15:00
Persistent=true

[Install]
WantedBy=timers.target
```

## Norn Pairing

Keep Norn discovery and backup freshness separate but adjacent:

```bash
contextdb norn drift \
  --manifest-url "$NORN_MANIFEST_URL" \
  --endpoint "$CONTEXTDB_REST_URL"

contextdb doctor \
  --url "$CONTEXTDB_REST_URL" \
  --backup-marker "$CONTEXTDB_BACKUP_MARKER" \
  --max-backup-age 24h
```

Norn drift tells you whether the live service registration still matches the expected endpoint. Doctor tells you whether the registered service is healthy and has recent backup evidence.

## Retention

Keep at least one recent local backup, one recent off-host copy, and the latest marker file. Start with a dry-run lifecycle retention report before deleting anything:

```bash
contextdb snapshot lifecycle retention \
  --dir "$CONTEXTDB_BACKUP_DIR" \
  --namespace "$CONTEXTDB_NAMESPACE" \
  --keep 14 \
  --report
```

The retention report scans `*.lifecycle.json` summaries, groups each lifecycle summary with its backup, manifest, rehearsal, promotion, and receipt-check artifacts, then marks the newest bundles as `keep` and older bundles as `pruneable`. It does not delete files.

When you want a reviewable cleanup plan, emit a shell script instead of JSON:

```bash
contextdb snapshot lifecycle retention \
  --dir "$CONTEXTDB_BACKUP_DIR" \
  --namespace "$CONTEXTDB_NAMESPACE" \
  --keep 14 \
  --emit-delete-script
```

The generated script contains only `rm -- ...` commands for existing artifacts in `pruneable` bundles. Review it before running it; contextdb still does not delete files.

Write a compact manifest index when you want one portable catalog of the backup directory:

```bash
contextdb snapshot lifecycle index \
  --dir "$CONTEXTDB_BACKUP_DIR" \
  --namespace "$CONTEXTDB_NAMESPACE" \
  --keep 14 \
  --out "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --report
```

The index records bundle timestamps, retention decisions, delete-plan commands, artifact paths, sizes, and SHA-256 hashes. If `--out` is omitted, contextdb writes `contextdb-backups.index.json` inside `--dir`.

Verify an existing index before trusting it during audit or cleanup:

```bash
contextdb snapshot lifecycle index verify \
  --in "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --report
```

Index verification re-checks indexed artifact paths, byte sizes, and SHA-256 hashes. Missing files, size drift, and checksum drift exit non-zero.

Compare two indexes when you want to review backup catalog drift across runs or hosts:

```bash
contextdb snapshot lifecycle index diff \
  --old "$OLD_BACKUP_DIR/contextdb-backups.index.json" \
  --new "$CONTEXTDB_BACKUP_DIR/contextdb-backups.index.json" \
  --report
```

Index diff reports added and removed bundles, retention decision changes, and artifact size or SHA-256 deltas. Any delta exits non-zero so automation can stop before cleanup or promotion.

A local cleanup pass can then remove old namespace backups after a successful export, lifecycle verification, off-host copy, and retention review:

```bash
find "$CONTEXTDB_BACKUP_DIR" \
  -name "${CONTEXTDB_NAMESPACE}-*.ndjson" \
  -mtime +14 \
  -delete
```

For production deployments, copy the timestamped NDJSON file to durable off-host storage before deleting local history.
