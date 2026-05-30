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

contextdb snapshot export \
  --namespace "$CONTEXTDB_NAMESPACE" \
  --out "$backup" \
  --backup-marker "$CONTEXTDB_BACKUP_MARKER"

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

The marker is written only after export succeeds. The dry-run restore report should show the expected record counts and node diff counts before the backup is considered ready for retention or off-host copy.

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

Keep at least one recent local backup, one recent off-host copy, and the latest marker file. A simple local retention pass can remove old namespace backups after a successful export, preview, and doctor check:

```bash
find "$CONTEXTDB_BACKUP_DIR" \
  -name "${CONTEXTDB_NAMESPACE}-*.ndjson" \
  -mtime +14 \
  -delete
```

For production deployments, copy the timestamped NDJSON file to durable off-host storage before deleting local history.
