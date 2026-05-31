---
title: Published Schemas
---

# Published Schemas

Published schemas give dashboards, automation, and handoff tooling stable contracts without requiring source checkout access.

## Catalog

Machine-readable catalog:

```bash
curl https://antiartificial.github.io/contextdb/schemas/index.json
```

| Schema | Status | Introduced | Cataloged | Owner | URL |
|:-------|:-------|:-----------|:----------|:------|:----|
| Retry fatigue presets | stable | v0.97.0 | v0.100.0 | review-handoff | [`/schemas/retry-fatigue-presets.schema.json`](/schemas/retry-fatigue-presets.schema.json) |

The catalog records the schema ID, docs-relative URL, canonical public URL, owning feature, owner area, and release provenance. Add new public contracts to `/schemas/index.json` when dashboards or automation should discover them without scraping docs pages.
