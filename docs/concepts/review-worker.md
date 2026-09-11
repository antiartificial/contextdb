---
title: Review Worker
---

# Review Worker

The review worker runs inside the ContextDB server and reads the existing review queue. Its default `rules` evaluator is deliberately conservative: it only resolves workflow entries already marked `stale` or `refuted`, and only when `resolve` is explicitly listed in `allowed_actions`. It holds low-confidence, contradictory, and acquisition-candidate entries for operator review.

Cycles are dry runs unless `execute` is set. A dry run reports planned decisions without mutating claims. Before an executed claim action, the worker persists an assigned `worker_started` decision; a mutation failure remains assigned for manual inspection and is not automatically repeated. Each cycle records a durable run summary and decisions. The per-namespace mutex prevents overlapping local cycles; Postgres advisory leases prevent overlapping cycles across server processes. A cycle is bounded to 30 seconds and at most 100 items. Remote store clients must invoke the server endpoint instead of executing cycles locally.

Run one cycle through the CLI without opening another database:

```bash
contextdb worker review --url "$CONTEXTDB_URL" --namespace production --once
contextdb worker review --url "$CONTEXTDB_URL" --namespace production --once --execute --allowed-actions resolve
```

The API is `POST /v1/namespaces/{namespace}/review/worker/cycle` and `GET /v1/namespaces/{namespace}/review/worker/runs`. The request selects `rules` or `webhook`, an execution flag, a bounded limit, and an explicit action allowlist. Webhooks use `CONTEXTDB_REVIEW_WEBHOOK_URL` and `CONTEXTDB_REVIEW_WEBHOOK_TOKEN` on the server; clients cannot supply endpoints or credentials. Responses are bounded, time-limited, and must contain an allowed action, confidence of at least 0.8, and a reason. No provider-specific adapters are included.

## Failure review

A run records `running`, then `completed` or `needs_attention`, including the evaluator and per-item decisions. If the process dies before the final summary, the durable running record remains evidence of an interrupted cycle. Assigned items are intentionally not automatically retried after an ambiguous mutation. Inspect `/recovery/pending`, reconcile any persistence intent, inspect claim history, then resolve the existing review decision. Reopening it blindly can repeat feedback that already took effect.

## Webhook contract

The server posts one review item to its configured evaluator. Return JSON such as:

```json
{"action":"validate","confidence":0.95,"reason":"Independent reviewed evidence supports this claim"}
```

Actions are `validate`, `refute`, `stale`, or `resolve`; only explicitly allowed actions execute. Confidence must be between zero and one; values below 0.8 cause abstention. Non-2xx, malformed, oversized, and canceled responses are recorded as errors. Acquisition candidates and actions without an applicable node are not sent through claim mutation. Configure access control before exposing operational endpoints beyond a trusted local environment.
