# Planned Optimizations

## 1. Write Deduplication Fingerprinting

**Priority:** High — cost savings at scale
**Effort:** ~1 afternoon

### Problem

Embedding is the most expensive operation per write. Real-world ingestion frequently re-ingests the same content (session replays, repeated crawls, agent retry loops). Every duplicate pays the full embedding cost.

### Design

Before the embedding pass, hash the content (normalized — lowercased, whitespace-collapsed, punctuation stripped) using SHA-256. If the fingerprint exists in the namespace, skip embedding entirely and update `transaction_time` on the existing node.

```
Write path:
  content → normalize(content) → SHA-256 → fingerprint

  IF fingerprint exists in namespace:
    update node.tx_time = now()
    return existing node_id (admitted: true, reason: "deduplicated")
  ELSE:
    proceed with normal write (embed → ingest → conflict check → admit)
```

### Implementation

1. Add `fingerprint TEXT` column to `nodes` table (Postgres) / field to BadgerDB node encoding
2. Add `idx_nodes_ns_fingerprint` unique index on `(namespace, fingerprint)` — rejects dupes at the DB level
3. In `NamespaceHandle.Write()`, compute fingerprint before calling embedder
4. Lookup by fingerprint; if found, touch `tx_time` and return early
5. Store fingerprint on new nodes

### Normalization function

```go
func contentFingerprint(s string) string {
    s = strings.ToLower(s)
    s = strings.Join(strings.Fields(s), " ")  // collapse whitespace
    s = stripPunctuation(s)                     // remove non-alphanumeric
    h := sha256.Sum256([]byte(s))
    return hex.EncodeToString(h[:])
}
```

### Edge cases

- Content that differs only in whitespace/punctuation → same fingerprint (intended)
- Same content, different source → dedup (fingerprint is content-only, source tracked separately)
- Same content, different confidence → dedup, keep higher confidence? Or keep existing? Design decision.
- Opt-out: `WriteRequest.SkipDedup bool` for cases where re-embedding is intentional (e.g., model changed)

---

## 2. Confidence Floor by Age

**Priority:** Medium — correctness improvement
**Effort:** ~2 hours

### Problem

A high-credibility source writes a claim once with 0.9 confidence. It never gets validated or refuted. Current exponential decay is utility-feedback-driven — if the node is never accessed, its utility decays, but confidence stays at 0.9 indefinitely. Stale high-confidence claims can mislead retrieval ranking.

### Design

Add a hard ceiling on confidence based on age:

```
effective_confidence = min(confidence, 1.0 - (age / max_age))
```

Where:
- `age` = `now - valid_from` (in hours)
- `max_age` = configurable per namespace mode (default: 8760 hours = 1 year)
- Floor clamps at a minimum (e.g., 0.05) so very old claims don't go to zero

### Implementation

1. Add `ConfidenceFloor` to `namespace.Config` with per-mode defaults:
   - `ModeBeliefSystem`: max_age = 8760h (1 year), min_floor = 0.1
   - `ModeAgentMemory`: max_age = 720h (30 days), min_floor = 0.05
   - `ModeGeneral`: max_age = 4380h (6 months), min_floor = 0.1
   - `ModeProcedural`: max_age = 17520h (2 years), min_floor = 0.2

2. Apply in the scoring pipeline (`internal/retrieval/scorer.go`) when computing `confidence_score`:
   ```go
   ageHours := time.Since(node.ValidFrom).Hours()
   ceiling := max(cfg.MinFloor, 1.0 - (ageHours / cfg.MaxAgeHours))
   effectiveConf := min(node.Confidence, ceiling)
   ```

3. This is a **read-time computation** — no schema changes, no migration. The stored confidence is untouched; the floor is applied during scoring only.

### Interaction with existing decay

- Exponential decay (utility-based) and confidence floor (age-based) are independent
- The final confidence score is `min(decayed_confidence, age_floor)` — whichever is lower wins
- This means a frequently-accessed old node still gets capped by age, and a rarely-accessed new node still decays from lack of utility

---

## 3. Query Result Score Breakdown

**Priority:** High — zero cost, high debugging value
**Effort:** ~1 hour

### Problem

Retrieval results return a single composite `score` but don't expose the per-dimension contributions. Debugging "why did this result rank here?" requires re-deriving the math manually. Building narrative retrieval or explanation features later requires this data.

### Design

Add `ScoreBreakdown` to `Result`:

```go
type ScoreBreakdown struct {
    Similarity  float64 `json:"similarity"`   // weighted similarity contribution
    Confidence  float64 `json:"confidence"`   // weighted confidence contribution
    Recency     float64 `json:"recency"`      // weighted recency contribution
    Utility     float64 `json:"utility"`      // weighted utility contribution
}
```

### Implementation

1. Add `ScoreBreakdown` field to `core.ScoredNode` and `client.Result`
2. In `internal/retrieval/scorer.go`, the weighted components are already computed individually before summing — just capture them:
   ```go
   breakdown := ScoreBreakdown{
       Similarity: params.SimilarityWeight * simScore,
       Confidence: params.ConfidenceWeight * confScore,
       Recency:    params.RecencyWeight * recScore,
       Utility:    params.UtilityWeight * utilScore,
   }
   ```
3. Include in REST API response (already returned per-result via `similarity_score`, `confidence_score`, etc. — but those are raw scores, not weighted contributions)
4. The breakdown shows `weight * raw_score` for each dimension, summing to the final `score`

### Distinction from existing fields

- `similarity_score` = raw cosine similarity (0-1)
- `breakdown.similarity` = `similarity_weight * similarity_score` (the actual contribution to ranking)
- Both are useful; the breakdown answers "how much did similarity matter for this result's rank?"

---

## 4. Namespace Warm/Cold Storage Tiering

**Priority:** Medium — latency stability at scale
**Effort:** ~1 day

### Problem

As namespaces accumulate claims, retrieval latency grows linearly with node count (brute-force in memory, index size in pgvector). Old, low-confidence claims that will never rank highly still participate in every retrieval query. Deletion loses data; decay alone doesn't remove from the search path.

### Design

Add a retrieval partition: nodes below a confidence threshold AND older than an age threshold are marked "cold" and excluded from default retrieval but remain queryable with an explicit flag.

```
Warm (default retrieval): confidence > cold_threshold OR age < cold_age
Cold (explicit only):     confidence <= cold_threshold AND age >= cold_age
```

### Implementation

1. Add `is_cold BOOLEAN DEFAULT false` to `nodes` table
2. Add a background job (or lazy check on write) that marks nodes cold when they cross both thresholds
3. Default retrieval adds `WHERE is_cold = false` (Postgres) or skips cold nodes in memory scan
4. `RetrieveRequest.IncludeCold bool` overrides to search everything
5. Cold nodes are still returned by `ValidAt()` and graph traversal — only vector/scored retrieval excludes them

### Configuration (per namespace)

```go
type ColdTierConfig struct {
    Enabled           bool
    ConfidenceThreshold float64       // e.g., 0.2
    AgeThreshold      time.Duration   // e.g., 720h (30 days)
    CheckInterval     time.Duration   // how often the background sweep runs
}
```

### Defaults by mode

| Mode | Confidence threshold | Age threshold |
|------|---------------------|---------------|
| BeliefSystem | 0.15 | 90 days |
| AgentMemory | 0.1 | 30 days |
| General | 0.2 | 180 days |
| Procedural | 0.1 | 365 days |

### Migration path

- `is_cold` defaults to `false` — all existing nodes start warm
- First sweep after enabling marks eligible nodes cold
- Reversible: set `Enabled: false` to include everything again, or `UPDATE nodes SET is_cold = false`

### Interaction with other features

- Confidence floor (feature #2) reduces effective confidence over time → more nodes cross the cold threshold naturally
- Deduplication (feature #1) prevents cold nodes from being re-created by re-ingestion
- Score breakdown (feature #3) shows when a result was pulled from cold storage (`retrieval_source: "cold"`)
