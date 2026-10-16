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

**Temporal contract — implemented, broader validation remains.** `RetrieveRequest` carries separate `ValidAt` and `KnownAt`; the DSL, APIs, SDKs, vector hydration, lexical candidates, and graph walks honor them. Node retractions append a new version. Repeated edge edits and invalidations now retain versions under one ID; memory and Badger conformance tests, Postgres integration tests, and a remote graph-walk test cover historical weights and endpoints. Postgres migration 003 preserves each existing edge as a baseline snapshot, but cannot reconstruct pre-migration edits. Historical vector search widens its candidate window, with a Badger exact-scan recovery when the ANN graph returns an incomplete window. Still evaluate larger datasets and fault behavior.

**Dedup provenance — version-checked source corrections.** Identical content from another source records versioned corroborating evidence on the canonical node without another embedding. `RetractSourceAssertion` removes one source's support in a new node version while preserving other corroboration; removing the final source retracts the node. Corroboration and source retraction now retry when another writer advances the node version. Postgres integration tests exercise competing source corrections across connections; the remote conditional-update RPC has a round-trip test. Source filters project surviving attribution and confidence into query results. Continue fault injection and larger source-credibility/correction evaluation. Keep dedup opt-in meanwhile.

The final-source node version and audit edge now commit as one operation in memory, Badger, and Postgres. A Postgres edge-conflict test verifies rollback of the node update. See the [v1 readiness gates](../plans/v1-readiness.md) for the remaining concurrency and restore proof.

**Evaluation baseline — expanded.** The seven-case corpus now requires the exact identifier at rank one; weighted fusion reaches MRR@5 0.893 on both memory and Badger in a local run. An RRF comparison remains observational and scores lower on this tiny corpus, so weighted fusion remains the default. Separate tests cover historical ANN underfill, source correction, edge invalidation, exact lexical boundaries, and reranker metadata preservation. Add larger realistic workloads, evidence-completeness scoring, remote parity, and repeated latency/cost runs before changing defaults.

### P1 — Retrieval coverage and agent consumption

**Exact-term retrieval — first implementation landed.** A namespace-scoped lexical candidate path covers identifiers, terms, and quoted phrases alongside vector, graph, and session candidates. All paths share temporal, label, source, and retraction filtering before final ranking. `CandidateK` and `RerankK` are independent of final `TopK`; reranking retains the original evidence score breakdown and path attribution. Weighted and RRF modes are available for evaluation, with weighted still the default. Add performance gates before using this path at large namespace sizes. Automatic graph seeds await reliable entity resolution.

Postgres now has a trigram candidate index covering historical node versions. Exact lexical ranking still checks the visible version after candidate selection. Memory and Badger still scan; performance and parity gates remain open.

**Token-budgeted results.** Add an optional output token budget alongside `TopK`. Select ranked results within the budget, report the counting method and any omission or truncation, and preserve IDs, citations, score breakdowns, and freshness metadata. Define whether metadata counts and how an oversized first result behaves. A token budget limits returned context, not search depth. Since the Go `Retrieve` method returns a slice, use a compatible result-envelope method or equivalent response design to expose budget metadata; define incremental counting for streams and consistent SDK/API behavior. Keep existing `TopK` behavior when no budget is supplied.

### P2 — Query-aware temporal retrieval

After the P0 temporal contract is verified, parse explicit and relative time expressions into bounded intervals. Require a clear valid-time or transaction-time interpretation; explicit caller filters take precedence over inferred dates. For broad periods, select relevant evidence across the interval rather than letting one dense ingestion batch dominate. Test time zones, ambiguous expressions, open intervals, and late-arriving evidence against explicit API queries.

### P3 — Evidence-backed, refreshable derived summaries

Extend existing episodic-to-semantic consolidation with optional, evidence-backed **derived summaries** of related claims. `Observation` already names a raw claim type in ContextDB, so do not reuse it for synthesized knowledge. Track supporting and contradicting node IDs, source lineage, covered transaction time, generation version, and freshness. Mark affected summaries stale on relevant writes, corrections, retractions, or source-trust changes; refresh asynchronously and preserve prior versions. Prevent generated summaries from citing themselves or each other as independent proof. Start with reviewed, opt-in namespaces; use evaluation to decide whether summaries belong in default retrieval.

## Sequencing and gates

1. Close the remaining P0 validation gaps: larger historical/source workloads, Postgres fault coverage, evidence-completeness evaluation, and corroboration ranking. Repeated edge edits, remote graph walks, and concurrent source retractions have direct tests. Continue the [v0.123.0 follow-ups](../releases/v0.123.0.md#next-steps) on indexed intent lookup, reviewer UI, and evaluator usefulness in their operational track.
2. Index lexical candidate generation for scale and add token-budgeted result envelopes as a separate P1 slice. Reassess fusion defaults only after a larger representative evaluation.
3. Add natural-language temporal intervals after explicit time queries are correct. Add derived summaries after change detection, provenance, and refresh recovery have a verified design.
4. Reconsider age penalties or storage tiering only when evaluations show a concrete failure or capacity threshold. Prefer explicit freshness/review signals and indexing before excluding evidence.
