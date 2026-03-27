---
title: Query DSL
parent: API Reference
nav_order: 7
---

# Query DSL

contextdb ships two query languages that compile to the same AST and map directly to `RetrieveRequest`. Both are available as Go functions in `internal/dsl`.

## Tier 1: Pipe Syntax

A pipeline of stages separated by `|`. Designed for the REPL and Admin UI.

```
search "project deadlines" | where valid_time > 7d ago | weight recency:high | top 10
```

### Stages

| Stage | Syntax | Description |
|:------|:-------|:------------|
| `search` | `search "text"` | Search text (required, must be first) |
| `where` | `where field op value [and ...]` | Predicate filters |
| `weight` | `weight dim:value [, ...]` | Score weight tuning |
| `top` | `top N` | Result limit |
| `expand` | `expand edge_type [depth N]` | Graph traversal |
| `rerank` | `rerank` | Enable LLM reranking |
| `in` | `in namespace [mode name]` | Namespace override |
| `as_of` | `as_of datetime` | Pin retrieval to historical time |
| `known_at` | `known_at datetime` | Transaction-time filter |
| `return` | `return field [, ...]` | Projection (field selection) |

### Examples

```bash
# Semantic search with confidence filter and recency boost
search "project deadlines" | where valid_time > 7d ago | weight recency:high | top 10

# Namespace-scoped temporal query
search "team headcount" | in agent_memory | as_of "2024-06-01" | known_at "2024-06-15"

# Graph traversal with reranking
search "Go routing" | where confidence > 0.7 | expand contradicts depth 2 | top 5 | rerank
```

---

## Tier 2: CQL (Contextual Query Language)

A keyword-oriented, SQL-adjacent language for embedding in apps and config files.

```sql
FIND "project status"
  IN NAMESPACE agent_memory
  WHERE valid_time > 7d ago AND source.credibility >= 0.7
  WEIGHT similarity=0.4, recency=0.4, confidence=0.2
  LIMIT 10
  RERANK
```

### Clauses

| Clause | Syntax | Required |
|:-------|:-------|:---------|
| `FIND` | `FIND "text"` | Yes |
| `IN` | `IN [NAMESPACE] name [MODE mode]` | No |
| `AS OF` | `AS OF datetime` | No |
| `KNOWN AT` | `KNOWN AT datetime` | No |
| `WHERE` | `WHERE bool_expr` | No |
| `FOLLOW` | `FOLLOW edge [DEPTH n] [, ...]` | No |
| `WEIGHT` | `WEIGHT dim=value [, ...]` | No |
| `LIMIT` | `LIMIT n` | No |
| `RERANK` | `RERANK [WITH "model"]` | No |
| `RETURN` | `RETURN field [, ...]` | No |

### WHERE expressions

CQL supports full boolean expressions with `AND`, `OR`, `NOT`, and parentheses:

```sql
WHERE confidence >= 0.7
  AND label IN ("hr", "org")
  AND valid_until IS NOT NULL

WHERE confidence BETWEEN 0.5 AND 1.0

WHERE (source.credibility > 0.8 OR label = "verified")
  AND valid_time > 30d ago
```

**Operators**: `=`, `!=`, `>`, `>=`, `<`, `<=`, `LIKE`, `BETWEEN`, `IN`, `IS NULL`, `IS NOT NULL`

### Examples

```sql
-- Full query with all clauses
FIND "project status"
  IN NAMESPACE agent_memory
  WHERE valid_time > 7d ago
    AND source.credibility >= 0.7
  WEIGHT similarity=0.4, recency=0.4, confidence=0.2
  LIMIT 10
  RERANK

-- Temporal point-in-time with graph traversal
FIND "Go routing patterns"
  AS OF "2024-06-01"
  KNOWN AT "2024-06-15"
  FOLLOW contradicts DEPTH 2
  RETURN content, source.name, valid_from, score

-- Label filtering with weight presets
FIND "team headcount"
  WHERE label IN ("hr", "org")
    AND confidence BETWEEN 0.5 AND 1.0
  WEIGHT utility=high
  LIMIT 5
```

---

## Shared Concepts

### Weight Dimensions

Both syntaxes support four weight dimensions. Values are `0.0`--`1.0` or named presets:

| Dimension | What it controls |
|:----------|:-----------------|
| `similarity` | Vector cosine similarity |
| `confidence` | Node epistemic confidence |
| `recency` | Freshness (exponential decay) |
| `utility` | Agent feedback / task outcome |

**Presets**: `high` = 0.8, `medium` = 0.5, `low` = 0.2, `off` = 0.0

### Datetime Values

Both syntaxes accept:

- ISO dates: `"2024-06-01"`, `"2024-06-01T15:04:05Z"`
- Relative times: `7d ago`, `2h ago`, `30m ago`
- Named times: `now`, `yesterday`, `today`, `last week`
- Time units: `s`, `m`, `h`, `d`, `w`, `mo`, `y` (or full words: `seconds`, `minutes`, etc.)

Relative times are resolved to absolute `time.Time` at parse time.

### Edge Types

Graph traversal supports: `contradicts`, `supports`, `derives_from`, `cites`

### Keywords

All keywords are **case-insensitive**. Pipe syntax uses lowercase by convention; CQL uses uppercase by convention. Only double quotes are accepted for strings. Single quotes are a parse error.

### Error Reporting

Parse errors include line and column numbers with keyword suggestions for typos (Levenshtein distance &le; 2):

```
parse error at 1:47: unexpected token "wher", did you mean "where"?
```

---

## Go API

```go
import "github.com/antiartificial/contextdb/internal/dsl"

// Pipe syntax
query, err := dsl.ParsePipe(`search "test" | where confidence > 0.7 | top 5`)

// CQL
query, err := dsl.ParseCQL(`FIND "test" WHERE confidence > 0.7 LIMIT 5`)

// Convert to RetrieveRequest
req := dsl.ToRetrieveRequest(query)
```

Both parsers produce the same `*dsl.Query` AST, which `ToRetrieveRequest` converts to a `client.RetrieveRequest` ready for the retrieval engine.
