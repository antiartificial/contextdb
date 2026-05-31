---
title: Ranking Baseline Retention Cookbook
---

# Ranking Baseline Retention Cookbook

Ranking baseline retention keeps release review artifacts useful without letting `.contextdb/ranking-baselines` grow forever. Use these recipes with `contextdb eval ranking --baseline-dir`, `--compare-baseline-dir`, `--baseline-retention-dir`, and `--emit-delete-script`.

## Choose A Keep Count

Pick `--baseline-retention-keep` from release rhythm and how often ranking behavior changes:

| Release rhythm | Suggested keep | Why |
|:---------------|:---------------|:----|
| Every tagged release | `5` | Keeps enough recent history for normal regressions without hoarding old release artifacts |
| Weekly release train | `8` | Covers roughly two months of weekly ranking movement |
| Active ranking tuning | `12` | Preserves more local experiments while score weights and corpus expectations are moving |
| Stable maintenance | `3` | Keeps current and near-previous baselines when ranking changes are rare |

Use a higher keep count when ranking changes are part of the current milestone. Lower it after the scoring model has stabilized.

## CI Artifact Pattern

Write baselines into a durable artifact directory:

```bash
contextdb eval ranking \
  --baseline-dir .contextdb/ranking-baselines \
  --markdown
```

Compare the current run against the latest previous baseline:

```bash
contextdb eval ranking \
  --compare-baseline-dir .contextdb/ranking-baselines \
  --diff-markdown
```

Upload `.contextdb/ranking-baselines/ranking-eval-v*.json` and `.contextdb/ranking-baselines/ranking-eval-v*.md` as CI artifacts for tagged releases. Keep the directory out of transient build caches so baseline history survives runner cleanup.

Write a machine-readable artifact inventory when CI needs durable evidence of what was retained:

```bash
contextdb eval ranking \
  --baseline-retention-dir .contextdb/ranking-baselines \
  --baseline-retention-keep 5 \
  --baseline-manifest-out ranking-baseline-manifest.json
```

The manifest records each JSON and Markdown baseline artifact with its version, retention status, path, existence, byte size, and SHA-256 hash. Missing counterpart artifacts are included with `missing: true` so release jobs can spot incomplete baseline pairs.

Verify the inventory later with:

```bash
contextdb eval ranking baseline manifest verify \
  --manifest ranking-baseline-manifest.json \
  --bundle-dir ranking-baseline-verification
```

The bundle directory contains `ranking-baseline-manifest-verification.json`, `ranking-baseline-manifest-verification.md`, and `ranking-baseline-manifest-annotations.txt` with stable names. The verifier exits non-zero when an artifact path is missing unexpectedly, points to a directory, has a different byte size, or no longer matches the recorded SHA-256 hash. Use `--markdown` for a stdout recap or `--markdown-out` to save the artifact summary beside the JSON report. Use `--annotations` or `--annotations-out` when CI should surface each failed artifact as an annotation line.

## GitHub Actions Annotation Recipe

Use this shape when a release job should keep machine-readable evidence, a human recap, and inline failure callouts from the same verification run:

```yaml
- name: Verify ranking baseline manifest
  run: |
    set +e
    contextdb eval ranking baseline manifest verify \
      --manifest ranking-baseline-manifest.json \
      --bundle-dir ranking-baseline-verification
    status=$?
    cat ranking-baseline-verification/ranking-baseline-manifest-annotations.txt
    exit "$status"

- name: Upload ranking baseline verification artifacts
  if: always()
  uses: actions/upload-artifact@v4
  with:
    name: ranking-baseline-verification
    path: ranking-baseline-verification/
```

`--annotations-out` is empty when verification passes. On failure, each line is formatted as a CI error annotation and includes the artifact path plus the version, kind, and validation message.

## Review Retention

Inspect retained and pruneable baseline versions before deleting anything:

```bash
contextdb eval ranking \
  --baseline-retention-dir .contextdb/ranking-baselines \
  --baseline-retention-keep 5
```

The newest baseline is marked current. The next newest versions up to the keep count are retained. Older versions are marked pruneable.

## Generate A Delete Script

After review, emit a shell script:

```bash
contextdb eval ranking \
  --baseline-retention-dir .contextdb/ranking-baselines \
  --baseline-retention-keep 5 \
  --emit-delete-script > prune-ranking-baselines.sh
```

Open the script before running it. It contains only `rm -- ...` commands for existing pruneable JSON and Markdown artifacts, and contextdb does not delete files itself.

## When To Keep More

Keep extra baselines when:

| Situation | Reason |
|:----------|:-------|
| New corpus category was added | Historical comparisons help separate corpus expansion from ranking regressions |
| Score weights changed | More history makes movement patterns easier to review |
| Retrieval source changed | Vector, graph, or session-source changes may affect different query categories |
| Release candidate is under investigation | Retain nearby artifacts until the regression or improvement is explained |

Once the investigation closes, rerun retention and regenerate the delete script.
