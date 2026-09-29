# ContextDB roadmap: retrieval, evidence, and memory quality

Updated 2026-09-29. This reconciles the earlier optimization proposals with current code and the proposed agent-memory work. It is a plan, not a release claim. The [feature matrix](../feature-matrix.md) is the implementation inventory; the [v0.123.0 follow-ups](../releases/v0.123.0.md#next-steps) remain operational work.

## Earlier proposals: disposition

| Proposal | Finding | Decision |
|:--|:--|:--|
| Write deduplication fingerprinting | Shipped as opt-in. A duplicate skips embedding; a different source now adds versioned corroborating evidence to the canonical node. | Remove the old implementation plan. Keep the default opt-in until representative evidence and source-filter evaluations justify changing it. |
| Confidence floor by age | Not implemented. Recency already decays with age; utility is separate. | Do not add the proposed ceiling. Evidence strength and freshness have different meanings. Evaluate explicit freshness or verification signals while preserving historical query behavior. |
| Query result score breakdown | Shipped across retrieval surfaces. | Remove from the active queue. Extend explanations as new stages are added. |
| Namespace warm/cold tiering | Not implemented; no scale threshold is documented here. | Defer pending a latency and capacity profile. Historical queries, conflict checks, and summary refresh must retain a complete-evidence path. |

The old designs remain in Git history. The old dedup plan assumed a unique fingerprint index, but the current Postgres index is non-unique. The old confidence-floor plan also conflated utility and recency decay. Neither assumption should guide implementation.

## Ranked work

Priority reflects correctness, user value, and dependency order. New capabilities stay opt-in until representative evaluations and supported-backend checks justify a default.

### P0 — Retrieval and evidence correctness

**Temporal contract — implemented foundation, validation remains.** `RetrieveRequest` now has separate `ValidAt` and `KnownAt`; the DSL, APIs, SDKs, vector hydration, and graph walks carry them through. `AsOf` remains a valid-time shorthand with current knowledge. Memory and Badger conformance tests cover late arrivals, corrections, and edges learned after their valid time; the Postgres CI smoke covers a late arrival. Next, add Postgres correction/retraction parity and version edge invalidations so a historical `KnownAt` can reconstruct an edge before it was invalidated. Measure historical vector recall when bounded ANN overfetch filters out many newer candidates.

**Dedup provenance — implemented foundation, evaluation remains.** Identical content from another source now records versioned corroborating evidence on the canonical node without another embedding. Source filters project surviving attribution and confidence into query results. Memory and Badger tests cover retries, history, and source filters. Next, evaluate the effect of corroboration on ranking and source credibility, and test correction or retraction of one source's assertion without removing independent evidence. Keep dedup opt-in meanwhile.

**Evaluation baseline — deterministic core added, coverage remains.** A separate seven-case corpus now records exact-term, temporal, contradiction, and source-dispute outcomes with rank, MRR, local latency, and memory/Badger parity. The exact-identifier miss is an explicit baseline. Next, connect it to the broader ranking workflow, add evidence-completeness and Postgres/remote runs, and compare both default and optional reranker paths. Define how reranker scores relate to the current score breakdown before changing fusion defaults.

### P1 — Retrieval coverage and agent consumption

**Exact-term retrieval.** Add a namespace-scoped lexical candidate path for names, identifiers, technical terms, and quoted phrases alongside vector, graph, and session candidates. Apply label, source, retraction, and temporal filters consistently. Keep enough candidates until fusion and optional reranking finish. Compare current weighted fusion against reciprocal rank fusion (RRF); adopt RRF only if evaluation shows better quality without unacceptable latency or loss of credibility-aware ranking. Expose path attribution without presenting rank consensus as source trust.

**Token-budgeted results.** Add an optional output token budget alongside `TopK`. Select ranked results within the budget, report the counting method and any omission or truncation, and preserve IDs, citations, score breakdowns, and freshness metadata. Define whether metadata counts and how an oversized first result behaves. A token budget limits returned context, not search depth. Since the Go `Retrieve` method returns a slice, use a compatible result-envelope method or equivalent response design to expose budget metadata; define incremental counting for streams and consistent SDK/API behavior. Keep existing `TopK` behavior when no budget is supplied.

### P2 — Query-aware temporal retrieval

After the P0 temporal contract is verified, parse explicit and relative time expressions into bounded intervals. Require a clear valid-time or transaction-time interpretation; explicit caller filters take precedence over inferred dates. For broad periods, select relevant evidence across the interval rather than letting one dense ingestion batch dominate. Test time zones, ambiguous expressions, open intervals, and late-arriving evidence against explicit API queries.

### P3 — Evidence-backed, refreshable derived summaries

Extend existing episodic-to-semantic consolidation with optional, evidence-backed **derived summaries** of related claims. `Observation` already names a raw claim type in ContextDB, so do not reuse it for synthesized knowledge. Track supporting and contradicting node IDs, source lineage, covered transaction time, generation version, and freshness. Mark affected summaries stale on relevant writes, corrections, retractions, or source-trust changes; refresh asynchronously and preserve prior versions. Prevent generated summaries from citing themselves or each other as independent proof. Start with reviewed, opt-in namespaces; use evaluation to decide whether summaries belong in default retrieval.

## Sequencing and gates

1. Close the remaining P0 validation gaps: Postgres correction/retraction parity, historical edge invalidation, corroboration ranking and source-specific corrections, evidence-completeness evaluation, and reranker interpretation. Continue the [v0.123.0 follow-ups](../releases/v0.123.0.md#next-steps) on indexed intent lookup, Postgres fault coverage, reviewer UI, and evaluator usefulness in their operational track.
2. Ship lexical retrieval and token budgets as independent P1 slices. Compare fusion approaches; keep final ranking explainable after any reranker.
3. Add natural-language temporal intervals after explicit time queries are correct. Add derived summaries after change detection, provenance, and refresh recovery have a verified design.
4. Reconsider age penalties or storage tiering only when evaluations show a concrete failure or capacity threshold. Prefer explicit freshness/review signals and indexing before excluding evidence.
