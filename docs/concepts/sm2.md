---
title: Active Recall (SM-2)
---

# Active Recall (SM-2)

contextdb uses the [SuperMemo-2](https://en.wikipedia.org/wiki/SuperMemo#Description_of_SM-2_algorithm) spaced repetition algorithm to boost the utility of memories that are actively recalled. Memories that are retrieved successfully gain higher utility scores; memories that fail recall decay faster.

## How it works

Each node can carry SM-2 state in its properties:

| Field | Description | Default |
|:------|:------------|:--------|
| `EasinessFactor` | How quickly review intervals grow (1.3--2.5) | 2.5 |
| `IntervalDays` | Days until next review | 0 |
| `RepetitionCount` | Consecutive successful recalls | 0 |
| `NextReviewDate` | When the memory is next due | unscheduled |
| `LastQuality` | Most recent quality rating (0--5) | 0 |

### Quality ratings

| Rating | Meaning |
|:-------|:--------|
| 5 | Perfect response |
| 4 | Correct after hesitation |
| 3 | Correct with serious difficulty |
| 2 | Incorrect, but seemed easy |
| 1 | Incorrect, remembered the answer |
| 0 | Complete blackout |

Ratings below 3 count as failure and reset the repetition count.

### Interval progression

1. First successful recall: review in **1 day**
2. Second: review in **6 days**
3. Subsequent: previous interval x EasinessFactor

The easiness factor adjusts after every review using the SM-2 formula:

```
EF' = EF + (0.1 - (5 - q) * (0.08 + (5 - q) * 0.02))
```

EF is floored at 1.3 to prevent items from becoming unreviewable.

## Background worker

The `RecallWorker` (in `internal/compact/recall.go`) runs as a background process that:

1. Scans for nodes with `NextReviewDate <= now`
2. Simulates retrieval quality based on the node's current confidence, utility, and recency
3. Updates SM-2 state and persists it back to the node's properties
4. Adjusts the node's utility score based on recall success

This means frequently-recalled, easy-to-retrieve memories naturally float to the top of retrieval results over time, while rarely-accessed memories gradually lose utility weight.

## Priority scoring

When multiple memories are due for review, `PriorityScore` ranks them by urgency:

```go
sm2 := core.Sm2FromProperties(node.Properties)
if sm2.IsDue(time.Now()) {
    priority := sm2.PriorityScore(time.Now())
    // priority [0, 1]: higher = more urgent
    // Factors: how overdue, easiness factor, repetition count
}
```

Priority increases with:
- **Overdue time** — memories overdue by 30+ days get maximum urgency
- **Low easiness factor** — harder items are prioritized (they need more practice)
- **Low repetition count** — items with fewer successful recalls are more fragile

## Example

```go
// Initialize SM-2 state for a new memory
sm2 := core.DefaultSM2Data()

// After a successful recall (quality 4 = correct with hesitation)
sm2 = sm2.Update(4)
// sm2.IntervalDays == 1, sm2.RepetitionCount == 1

// After another success (quality 5 = perfect)
sm2 = sm2.Update(5)
// sm2.IntervalDays == 6, sm2.RepetitionCount == 2

// After a third success
sm2 = sm2.Update(5)
// sm2.IntervalDays == 15 (6 * 2.5), sm2.RepetitionCount == 3

// After a failure (quality 1 = incorrect)
sm2 = sm2.Update(1)
// sm2.IntervalDays == 1, sm2.RepetitionCount == 0 (reset)

// Persist to node properties
props := sm2.ToProperties(node.Properties)
```

## Integration with scoring

The utility component of the [scoring function](scoring) is where SM-2 has its effect. Nodes with high recall success (high utility) score higher in retrieval. The `agent_memory` namespace mode weights utility at 20%, making active recall particularly impactful for agentic workflows.
