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
  --report
```

Pair this with `contextdb doctor --kv-key context:prod:support:recent-nodes` when you want the combined health report to verify the hot key exists. Use `contextdb doctor --kv-derived-key context:prod:support:recent-nodes --max-kv-derived-age 2h` when you also want doctor to validate the derived value's `generated_at` freshness.

## Common Patterns

| Pattern | Key | Scope |
|:--------|:----|:------|
| Per-environment session context | `context:prod:support:session-context` | Namespace only |
| Label-specific recent facts | `context:prod:support:recent-nodes:incident` | Namespace plus `Incident` label |
| Review handoff context | `context:prod:reviews:recent-nodes` | Review namespace plus handoff labels |

Keep derived values small enough for the consumer to read quickly. For prompt context, start with `--derive-limit 5`; for dashboards or workers, use a higher limit only after checking payload size.
