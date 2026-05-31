---
title: Playground
---

# Playground

The hosted kitchen-sink playground is the separate [`antiartificial/context-kitchen-sink`](https://github.com/antiartificial/context-kitchen-sink) app. It is the interactive demo surface for scenarios, source credibility, contradiction workflows, agent memory, auditor epistemics, DSL exploration, and current system checks.

This page is the companion CLI checklist for trying the same major contextdb surfaces from a local server.

## Start a local server

```bash
go run ./cmd/contextdb \
  --rest-addr :7701 \
  --observe-addr :7702
```

Open the admin dashboard:

```bash
open http://localhost:7702/admin/
```

## Health and feature surface

```bash
curl http://localhost:7701/v1/ping
curl http://localhost:7701/v1/version
curl http://localhost:7701/v1/features
curl http://localhost:7702/admin/api/metrics
```

## Write and retrieve

```bash
curl -X POST http://localhost:7701/v1/namespaces/playground/write \
  -H "Content-Type: application/json" \
  -d '{
    "content": "contextdb combines vector search, graph evidence, source trust, recency, and utility.",
    "source_id": "playground/docs",
    "labels": ["Claim", "Playground"],
    "confidence": 0.92
  }'

curl -X POST http://localhost:7701/v1/namespaces/playground/retrieve \
  -H "Content-Type: application/json" \
  -d '{
    "text": "How does contextdb rank knowledge?",
    "top_k": 5,
    "score_params": {
      "similarity_weight": 0.4,
      "confidence_weight": 0.35,
      "recency_weight": 0.15,
      "utility_weight": 0.1
    }
  }'
```

## Explain ranking

After writing at least two related nodes, compare their ranking factors:

```bash
curl -X POST http://localhost:7701/v1/namespaces/playground/rank/explain \
  -H "Content-Type: application/json" \
  -d '{
    "node_id": "NODE_UUID",
    "other_node_id": "OTHER_NODE_UUID",
    "text": "How does contextdb rank knowledge?",
    "max_depth": 2
  }'
```

The response includes a summary, margin, score-factor deltas, and graph evidence when available.

## Ranking evaluation

```bash
contextdb eval ranking --out ranking-eval.json --report
contextdb eval ranking --markdown-out ranking-eval.md
contextdb eval ranking --compare ranking-eval.json --diff-markdown
```

In the admin UI, open **Ranking Evaluation**, choose a `Top K`, and optionally load a previous `ranking-eval-v*.json` baseline for an in-browser delta.

## Knowledge acquisition dry-run

```bash
curl -X POST http://localhost:7701/v1/namespaces/playground/acquisition/plan \
  -H "Content-Type: application/json" \
  -d '{"budget": 5, "max_gaps": 3}'

curl -X POST http://localhost:7701/v1/namespaces/playground/acquisition/execute \
  -H "Content-Type: application/json" \
  -d '{
    "budget": 2,
    "max_results": 3,
    "allowed_source_ids": ["playground/docs"],
    "connectors": [
      {
        "id": "local-search-preview",
        "type": "search",
        "endpoint": "https://search.example.internal/contextdb",
        "allowed_source_ids": ["playground/docs"],
        "default_labels": ["acquired", "playground"]
      }
    ]
  }'
```

Add `"execute": true` only after the dry-run payload is reviewed and the connector endpoint is real.

## Provider-backed connector server

```bash
OPENAI_API_KEY=... \
XAI_API_KEY=... \
ANTHROPIC_API_KEY=... \
contextdb connectors serve \
  --addr :7780 \
  --providers openai,xai,anthropic \
  --allowed-domains docs.example.com,github.com
```

Then use an endpoint such as `http://localhost:7780/openai/search` in the acquisition execution connector list.

## Receipts and retry guidance

```bash
curl "http://localhost:7701/v1/namespaces/playground/acquisition/receipts"
curl "http://localhost:7701/v1/namespaces/playground/acquisition/retry-candidates"
curl "http://localhost:7701/v1/namespaces/playground/acquisition/retry-recommendations"
```

## Debugger workflow

Use the admin UI for the most complete debugger experience:

1. Open `http://localhost:7702/admin/`.
2. Search the `playground` namespace.
3. Inspect a node to view source context, confidence history, contradiction paths, and graph neighbors.
4. Add two search results to **Explain Rank** to compare score factors.

## Release and docs checks

```bash
npm run docs:build
contextdb docs schema-catalog verify --report --annotations
contextdb eval ranking baseline manifest verify --manifest ranking-baseline-manifest.json --report
contextdb closure-bundle verify --manifest closure-bundle-manifest.json --report
```

These commands match the current reliability direction: local docs build, published schema catalog verification, ranking baseline artifact verification, and closure bundle verification.

## Hosted kitchen sink status

The current playground repo now includes a **System** tab for:

- Current contextdb version and feature-surface status.
- Acquisition connector dry-run preview.
- Acquisition receipt and retry recommendation inspection.
- Explain-rank comparison over seeded auditor claims.

The full Svelte admin dashboard still lives in the main contextdb server on observe port `7702`. That dashboard is the first-class place for ranking evaluation, metrics, source-trust timelines, contradiction-path summaries, graph/source context, and debugger compare workflows.

The older `test_contextdb.py` script in this repository is only a lightweight illustrative smoke script and does not cover current features such as acquisition connectors, receipt retries, ranking baselines, schema catalog verification, or the Svelte debugger.
