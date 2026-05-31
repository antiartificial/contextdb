---
title: KV Derivation Recipes
---

# KV Derivation Recipes

KV derivation recipes help operators refresh reviewed hot keys from graph data without hand-writing session context payloads. The first supported derivation, `recent-nodes`, builds a compact JSON value from valid graph nodes and stays dry-run first.

## Name Stable Hot Keys

Use key names that describe the audience, namespace, and value shape:

```text
context:{environment}:{namespace}:recent-nodes
context:{environment}:{namespace}:recent-nodes:{label}
context:{environment}:{namespace}:session-context
```

Prefer stable keys for consumers and put fast-changing scope in the value metadata. Use label-specific keys only when separate consumers need separate cache values.

## Derive Recent Nodes

Start with a dry-run report:

```bash
contextdb repair kv-cache \
  --key context:prod:support:recent-nodes \
  --derive recent-nodes \
  --derive-namespace support \
  --derive-label SessionContext \
  --derive-limit 5 \
  --report
```

The derived value includes `kind`, `namespace`, `generated_at`, `limit`, `labels`, `count`, and a compact node list with IDs, labels, text, confidence, validity timestamps, and source IDs.

## Review Before Execute

Before adding `--execute`, check:

| Field | Review question |
|:------|:----------------|
| `namespace` | Does it match the consumer's namespace? |
| `labels` | Is the scope narrow enough for the hot key? |
| `count` | Is the result empty, unexpectedly large, or missing expected facts? |
| `generated_at` | Is the value from this review window? |
| Node text | Is the payload appropriate for downstream prompts, dashboards, or workers? |

If the report is too broad, add `--derive-label`. If it is too sparse, remove the label filter or increase `--derive-limit`.

## Promote The Reviewed Value

Execute only after the dry-run payload is accepted:

```bash
contextdb repair kv-cache \
  --key context:prod:support:recent-nodes \
  --derive recent-nodes \
  --derive-namespace support \
  --derive-label SessionContext \
  --derive-limit 5 \
  --execute \
  --receipt-out context-prod-support-recent-nodes.receipt.json \
  --report
```

Pair this with `contextdb doctor --kv-key context:prod:support:recent-nodes` when you want the combined health report to verify the hot key exists. Use `contextdb doctor --kv-derived-key context:prod:support:recent-nodes --max-kv-derived-age 2h` when you also want doctor to validate the derived value's `generated_at` freshness.

If the freshness check fails, review the `recommended_repair_command` in the doctor detail. It stays dry-run by default and points back to `contextdb repair kv-cache --derive recent-nodes` with the checked key and inferred derive namespace.

## Execute A Stale Derived KV Repair

When `contextdb doctor --kv-derived-key ...` reports stale, missing, or malformed derived metadata, treat the repair as a reviewed refresh rather than an automatic rewrite:

1. Save the doctor report with the failing `kv_derived_freshness` detail and `recommended_repair_command`.
2. Run the recommended command exactly as shown and keep it dry-run with `--report`.
3. Review the derived payload fields in the report: `kind`, `namespace`, `labels`, `count`, `generated_at`, and the compact node list.
4. Re-run the same command with `--execute --report` only after the payload matches the intended namespace, label scope, and consumer.
5. Re-run doctor with the original `--kv-derived-key` and `--max-kv-derived-age` values to confirm the refreshed value is present and fresh.

The reviewed execution usually looks like this:

```bash
contextdb repair kv-cache \
  --key context:prod:support:recent-nodes \
  --derive recent-nodes \
  --derive-namespace support \
  --derive-label SessionContext \
  --derive-limit 5 \
  --execute \
  --report

contextdb doctor \
  --kv-derived-key context:prod:support:recent-nodes \
  --max-kv-derived-age 2h \
  --report
```

If the dry-run payload is wrong, fix the derivation inputs before executing. Common corrections are adding `--derive-label`, changing `--derive-namespace`, lowering `--derive-limit`, or choosing a more specific hot-key name for the consumer.

`--receipt-out` is only valid with `--execute --derive recent-nodes`. The receipt records the executed refresh report, the SHA-256 hash of the reviewed derived value, and a `contextdb doctor --kv-derived-key ... --report` command to confirm the refreshed keys.

## Common Patterns

| Pattern | Key | Scope |
|:--------|:----|:------|
| Per-environment session context | `context:prod:support:session-context` | Namespace only |
| Label-specific recent facts | `context:prod:support:recent-nodes:incident` | Namespace plus `Incident` label |
| Review handoff context | `context:prod:reviews:recent-nodes` | Review namespace plus handoff labels |

Keep derived values small enough for the consumer to read quickly. For prompt context, start with `--derive-limit 5`; for dashboards or workers, use a higher limit only after checking payload size.
