---
title: Benchmarks
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

# Representative corpus ranking snapshot
contextdb eval ranking --out ranking-eval.json --report

# Human-readable ranking release recap
contextdb eval ranking --markdown-out ranking-eval.md

# Release-to-release ranking diff
contextdb eval ranking --compare previous-ranking-eval.json --diff-markdown

# Write versioned JSON and Markdown ranking baselines
contextdb eval ranking --baseline-dir .contextdb/ranking-baselines

# Compare against the latest previous baseline in that directory
contextdb eval ranking --compare-baseline-dir .contextdb/ranking-baselines --diff-markdown

# Inspect retained and pruneable ranking baselines
contextdb eval ranking --baseline-retention-dir .contextdb/ranking-baselines --baseline-retention-keep 5

# Emit a reviewable shell script for pruneable baseline artifacts
contextdb eval ranking --baseline-retention-dir .contextdb/ranking-baselines --baseline-retention-keep 5 --emit-delete-script
```

## Ranking Eval Snapshots

`contextdb eval ranking` emits a JSON snapshot for the representative corpus in `testdata/corpus.go`. The report includes top-k results for each labelled query, expected rank, reciprocal rank, MRR, raw score components, weighted score breakdowns, and retrieval source. Use it before and after ranking changes to review score drift even when the golden corpus still passes.

Use `--markdown` or `--markdown-out` when you want a compact release-review recap with pass/fail totals, MRR, failure details, top results, and score breakdowns.

Use `--compare` with a previous JSON snapshot when you want a historical diff. Add `--diff-report`, `--diff-out`, `--diff-markdown`, or `--diff-markdown-out` to capture MRR deltas, pass-count changes, pass flips, largest rank movements, and largest top-score movements.

Use `--baseline-dir` when you want release-friendly baseline artifacts. The command writes both `ranking-eval-vX.Y.Z.json` and `ranking-eval-vX.Y.Z.md` for the current contextdb version. Use `--compare-baseline-dir` to resolve the latest previous `ranking-eval-vX.Y.Z.json` in that directory and compare the current ranking run against it. This keeps release ranking reviews consistent without hand-picking a baseline filename each time.

Use `--baseline-retention-dir` when you want a read-only retention report for versioned ranking baselines. The report marks the newest baseline as current, retains the newest `--baseline-retention-keep` versions, and lists older versions as pruneable without deleting files. Add `--emit-delete-script` to print a shell script containing only `rm -- ...` commands for existing pruneable JSON and Markdown artifacts; review the script before running it. See the [ranking baseline retention cookbook](/deployment/ranking-baseline-retention-cookbook) for keep-count and CI artifact recipes.

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
