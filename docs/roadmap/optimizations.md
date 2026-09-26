# ContextDB roadmap: retrieval, evidence, and memory quality

Updated 2026-09-26. This reconciles the earlier optimization proposals with current code and the proposed agent-memory work. It is a plan, not a release claim. The [feature matrix](../feature-matrix.md) is the implementation inventory; the [v0.123.0 follow-ups](../releases/v0.123.0.md#next-steps) remain operational work.

## Earlier proposals: disposition

| Proposal | Finding | Decision |
|:--|:--|:--|
| Write deduplication fingerprinting | Shipped as opt-in. A duplicate skips embedding and touches an existing node. | Remove the old implementation plan. Audit whether a repeat from a different source should record independent evidence; current dedup returns before source resolution. Keep the default opt-in until this is resolved. |
| Confidence floor by age | Not implemented. Recency already decays with age; utility is separate. | Do not add the proposed ceiling. Evidence strength and freshness have different meanings. Evaluate explicit freshness or verification signals while preserving historical query behavior. |
| Query result score breakdown | Shipped across retrieval surfaces. | Remove from the active queue. Extend explanations as new stages are added. |
| Namespace warm/cold tiering | Not implemented; no scale threshold is documented here. | Defer pending a latency and capacity profile. Historical queries, conflict checks, and summary refresh must retain a complete-evidence path. |

The old designs remain in Git history. The old dedup plan assumed a unique fingerprint index, but the current Postgres index is non-unique. The old confidence-floor plan also conflated utility and recency decay. Neither assumption should guide implementation.

## Ranked work

Priority reflects correctness, user value, and dependency order. New capabilities stay opt-in until representative evaluations and supported-backend checks justify a default.

### P0 — Retrieval and evidence correctness

**Temporal contract.** Specify separately how valid time (when a claim was true) and transaction time (when ContextDB knew it) apply to candidate selection, graph hydration, filters, and ranking. Today `RetrieveRequest` exposes one `AsOf`; the DSL parses `KnownAt` but does not carry it into that request. Resolve this before interpreting natural-language dates. Test late-arriving facts, corrections, retractions, and historical reads on memory, BadgerDB, and Postgres.

**Dedup provenance.** Decide whether identical content from another source is corroborating evidence. If so, record that source assertion or event without paying for another embedding, while retaining idempotent retries and historical transaction information. Verify attribution and credibility behavior before changing the opt-in default.

**Evaluation baseline.** Add exact-term, temporal, contradiction, and source-dispute cases to the existing ranking workflow. Capture relevance, evidence completeness, latency, and backend parity before changing fusion or query interpretation. Include current and optional reranker paths; the reranker currently returns its own scores, so define what its score explanation means.

### P1 — Retrieval coverage and agent consumption

**Exact-term retrieval.** Add a namespace-scoped lexical candidate path for names, identifiers, technical terms, and quoted phrases alongside vector, graph, and session candidates. Apply label, source, retraction, and temporal filters consistently. Keep enough candidates until fusion and optional reranking finish. Compare current weighted fusion against reciprocal rank fusion (RRF); adopt RRF only if evaluation shows better quality without unacceptable latency or loss of credibility-aware ranking. Expose path attribution without presenting rank consensus as source trust.

**Token-budgeted results.** Add an optional output token budget alongside `TopK`. Select ranked results within the budget, report the counting method and any omission or truncation, and preserve IDs, citations, score breakdowns, and freshness metadata. Define whether metadata counts and how an oversized first result behaves. A token budget limits returned context, not search depth. Since the Go `Retrieve` method returns a slice, use a compatible result-envelope method or equivalent response design to expose budget metadata; define incremental counting for streams and consistent SDK/API behavior. Keep existing `TopK` behavior when no budget is supplied.

### P2 — Query-aware temporal retrieval

After the P0 temporal contract is verified, parse explicit and relative time expressions into bounded intervals. Require a clear valid-time or transaction-time interpretation; explicit caller filters take precedence over inferred dates. For broad periods, select relevant evidence across the interval rather than letting one dense ingestion batch dominate. Test time zones, ambiguous expressions, open intervals, and late-arriving evidence against explicit API queries.

### P3 — Evidence-backed, refreshable derived summaries

Extend existing episodic-to-semantic consolidation with optional, evidence-backed **derived summaries** of related claims. `Observation` already names a raw claim type in ContextDB, so do not reuse it for synthesized knowledge. Track supporting and contradicting node IDs, source lineage, covered transaction time, generation version, and freshness. Mark affected summaries stale on relevant writes, corrections, retractions, or source-trust changes; refresh asynchronously and preserve prior versions. Prevent generated summaries from citing themselves or each other as independent proof. Start with reviewed, opt-in namespaces; use evaluation to decide whether summaries belong in default retrieval.

## Sequencing and gates

1. Complete P0 time and provenance decisions and establish baseline cases. Continue the [v0.123.0 follow-ups](../releases/v0.123.0.md#next-steps) on indexed intent lookup, Postgres fault coverage, reviewer UI, and evaluator usefulness in their operational track.
2. Ship lexical retrieval and token budgets as independent P1 slices. Compare fusion approaches; keep final ranking explainable after any reranker.
3. Add natural-language temporal intervals after explicit time queries are correct. Add derived summaries after change detection, provenance, and refresh recovery have a verified design.
4. Reconsider age penalties or storage tiering only when evaluations show a concrete failure or capacity threshold. Prefer explicit freshness/review signals and indexing before excluding evidence.
