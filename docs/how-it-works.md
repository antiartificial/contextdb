---
title: How ContextDB Works
description: Three interactive explanations of writing, retrieving, and reviewing evidence in ContextDB.
sidebar: false
aside: false
pageClass: contextdb-walkthrough
---

# How ContextDB works

Follow one deployment claim through three processes. Choose a step under each figure to inspect it, pause the animation, or change its speed. On a narrow screen, use each figure's Full screen control to see more detail. The figures describe capabilities in the current code; [planned retrieval and memory work](/roadmap/optimizations) is separate.

## 1. A claim enters

An application writes “Deploys use canary rollout” with a source. ContextDB can embed its content, resolves the source's credibility, and checks nearby claims. The admission gate can reject a weak or duplicate claim. An admitted claim is checked for contradictions and written to the graph, vector index, and event log.

<figure>
  <ClientOnly>
    <ContextDBFlow story="write" title="Interactive flow of a claim through embedding, source resolution, admission, conflict detection, and storage" />
  </ClientOnly>
  <figcaption>Try Submit, Check, and Persist. A contradiction links competing claims for inspection.</figcaption>
</figure>

With opt-in fingerprint deduplication, a repeat from the same source touches the existing node. A different source adds versioned corroborating evidence to that node without another embedding. [Write path details](/architecture/write-path)

## 2. A question finds evidence

“How do we deploy?” can draw candidates from vector search, a graph walk when seed IDs are provided, and recent session context. ContextDB merges candidates and scores them for similarity, confidence, recency, and utility. Default scoring exposes a weighted score breakdown. An optional reranker can alter the final order and returns its own scoring explanation.

<figure>
  <ClientOnly>
    <ContextDBFlow story="retrieve" title="Interactive flow of a question through vector, graph, and session retrieval paths into ranking" />
  </ClientOnly>
  <figcaption>Try Find and Rank. Each path contributes candidates; ranking decides what appears first.</figcaption>
</figure>

Exact-term search, reciprocal rank fusion, natural-language time parsing, and token-budgeted results are [roadmap items](/roadmap/optimizations), not parts of this current flow. [Read path details](/architecture/read-path)

## 3. A disagreement becomes a decision

Suppose another source says “Deploys use blue-green.” A confirmed contradiction creates an edge between claims and can produce a review task. If a caller submits feedback, ContextDB records a durable event that can affect source credibility. The review queue derives tasks from contradictions and other evidence signals so an operator can inspect sources and record a decision.

<figure>
  <ClientOnly>
    <ContextDBFlow story="review" title="Interactive flow from contradictory claims through feedback and the review queue to an operator" />
  </ClientOnly>
  <figcaption>Try Disagree, Signal, and Resolve. Review does not silently select a winning claim.</figcaption>
</figure>

Review workflow decisions are recorded for later inspection. The current consolidation worker can create semantic summaries with links to source episodes; automatically refreshed, versioned derived summaries are [planned](/roadmap/optimizations#p3-evidence-backed-refreshable-derived-summaries). [Review worker details](/concepts/review-worker)
