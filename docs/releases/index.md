---
title: Releases
---

# Releases

Release notes are the high-level map of what changed, why it matters, and which docs to read next. The main docs track the latest stable release; feature tables include the version where major surfaces were introduced.

| Release | Date | Theme |
|:--------|:-----|:------|
| [v0.4.0](v0.4.0) | 2026-05-30 | Version introspection, durability coverage, ranking golden tests, and API contracts |
| [v0.3.0](v0.3.0) | 2026-05-29 | Graph inspection, feedback loops, explainability, and non-breaking dedup |
| v0.2.0 | 2026-03-30 | Query optimization capabilities |
| v0.1.0 | 2026-03-29 | Initial tagged release |

## Documentation Versioning

The docs are currently versioned by release notes and feature tags rather than a full version switcher. That keeps the latest docs easy to maintain while still making it clear when major capabilities landed.

Use the Git tags for exact historical source:

```bash
git checkout v0.4.0
npm ci
npm run docs:build
```

Full multi-version docs would make sense once there are active supported release lines with incompatible APIs. For now, v0.4.0 is intentionally non-breaking, so tagged release notes are the clearer tool.
