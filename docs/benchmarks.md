---
title: Benchmarks
nav_order: 8
---

# Benchmarks

contextdb includes multiple benchmark suites for evaluating retrieval quality, adversarial resistance, and long-term memory performance.

## Running benchmarks

```bash
# Full benchmark suite (writes HTML report)
make bench

# MTEB retrieval quality
make bench-mteb

# Adversarial resistance
make bench-adversarial

# LongMemEval (requires dataset download)
make longmemeval

# Fitness evaluation suite
make fitness
```

## MTEB retrieval quality

The MTEB benchmark (`bench/mteb/`) evaluates retrieval quality using standard information retrieval metrics:

| Metric | Description |
|:-------|:------------|
| **NDCG@10** | Normalized Discounted Cumulative Gain at rank 10 |
| **Recall@K** | Fraction of relevant documents retrieved at various K values |
| **MRR** | Mean Reciprocal Rank of the first relevant result |

The benchmark tests contextdb's hybrid retrieval (vector + graph fusion) against pure vector search baselines. It exercises the full pipeline: embedding, vector indexing, graph traversal, fusion, and scoring.

## Adversarial resistance

The adversarial benchmark (`bench/adversarial/`) tests contextdb's resilience to:

### Poisoning attacks
- Flood writes from low-credibility sources
- Near-duplicate injection with contradicting content
- Measures how many poison nodes pass the admission gate
- Validates that source credibility correctly throttles adversarial writes

### Temporal consistency
- Writes facts with overlapping validity windows
- Queries at specific points in time
- Validates that `AsOf` filtering correctly excludes expired or future facts
- Tests that bi-temporal ordering is preserved across updates

## LongMemEval

The LongMemEval benchmark (`bench/longmemeval/`) evaluates long-session memory:

- Loads or downloads the LongMemEval dataset from HuggingFace
- Simulates multi-turn conversations across extended sessions
- Measures recall of facts mentioned in earlier turns
- Tests the interaction between memory decay, consolidation, and retrieval

## Fitness suite

The fitness evaluation (`bench/fitness_test.go`) is a comprehensive end-to-end test:

- Loads the test corpus from `testdata/corpus.go`
- Writes all items through the admission gate
- Runs a battery of retrieval queries
- Validates correctness of results (expected items present, ordering reasonable)
- Measures latency percentiles for write and retrieval operations
- Tests scoring function behaviour across namespace modes

## Score benchmarks

`bench/score_bench_test.go` microbenchmarks the composite scoring function:

```bash
go test -bench=. ./bench/
```

This measures raw scoring throughput independent of storage backend.
