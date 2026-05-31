---
title: Retry Fatigue Cookbook
---

# Retry Fatigue Cookbook

Retry fatigue reports help operators route unresolved handoff webhook failures by endpoint, owner, and escalation lane. Use these examples when the same target endpoint serves multiple review workloads and the incident handoff needs a narrower view than the full endpoint summary.

## Focus One Owner

Use `owner` when a handoff should follow one workload owner:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/handoff-webhooks/retry-fatigue?owner=alice"
```

This keeps the endpoint grouping intact, but only includes unresolved retry recommendations tied to Alice's saved handoff digests.

## Focus One Escalation Lane

Use `escalation_level` when the handoff belongs to one escalation class:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/handoff-webhooks/retry-fatigue?escalation_level=review_overdue"
```

Common lanes include `review_overdue`, `source_trust_anomaly`, and any custom escalation value saved in the review handoff digest.

## Use A Preset Lane

Use `preset` when dashboards or handoff scripts should share a stable lane name:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/handoff-webhooks/retry-fatigue?preset=review-overdue"
```

Built-in presets are `review-overdue`, `source-trust-anomaly`, and `unassigned-review-overdue`. JSON responses include the same preset metadata so clients can render the available lanes without hard-coding descriptions.

## Preset Reference

Dashboards can read the same preset metadata from retry fatigue JSON responses, but this table is the compact reference for operators and handoff tooling:

| Preset | Expanded filters | Intended handoff audience |
|:-------|:-----------------|:--------------------------|
| `review-overdue` | `escalation_level=review_overdue` | Review owners or coordinators handling assigned or snoozed work that missed its review window |
| `source-trust-anomaly` | `escalation_level=source_trust_anomaly` | Source-quality reviewers investigating credibility drops, repeated refutations, or trust anomalies |
| `unassigned-review-overdue` | `owner=unassigned`, `escalation_level=review_overdue` | Queue triage or on-call reviewers who need to claim overdue work with no explicit owner |

Explicit `owner` or `escalation_level` query parameters can narrow or override a preset-expanded value. Keep preset names stable in dashboards and scripts; change the expanded filters in one place when the handoff lane changes.

## Focus An Owner And Lane

Combine both filters when the incident is specific to one owner inside one escalation class:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/handoff-webhooks/retry-fatigue?owner=alice&escalation_level=review_overdue"
```

Use this for narrow handoffs where the endpoint is noisy but the actionable work belongs to a single lane.

## Export Markdown For Handoffs

Add `format=markdown` to produce incident-ready notes:

```bash
curl "http://localhost:7701/v1/namespaces/my-app/review/handoff-webhooks/retry-fatigue?owner=alice&escalation_level=review_overdue&format=markdown"
```

The Markdown export includes endpoint totals, owner counts, escalation-level counts, status-family counts, and the latest failure details for the scoped fatigue view.

## GraphQL Dashboard Query

Use the same filters in GraphQL dashboards:

```graphql
query {
  reviewHandoffRetryFatigue(
    namespace: "my-app"
    preset: "review-overdue"
  ) {
    targetUrl
    candidates
    totalAttempts
    ready
    waiting
    owners {
      owner
      count
    }
    escalationLevels {
      escalationLevel
      count
    }
    lastStatusCode
    lastError
  }
}
```

Keep unfiltered endpoint fatigue dashboards for broad monitoring. Use filtered views for handoffs, paging lanes, and workload-specific follow-up.
