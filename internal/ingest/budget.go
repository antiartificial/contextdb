package ingest

import (
	"sync"
	"sync/atomic"
	"time"
)

// ConflictBudgetTracker tracks conflict detection rate per namespace.
type ConflictBudgetTracker struct {
	mu       sync.RWMutex
	counters map[string]*budgetCounter // ns → counter
}

type budgetCounter struct {
	count   atomic.Int64
	resetAt time.Time
	limit   int
}

// NewConflictBudgetTracker creates a new tracker.
func NewConflictBudgetTracker() *ConflictBudgetTracker {
	return &ConflictBudgetTracker{
		counters: make(map[string]*budgetCounter),
	}
}

// Allow checks if conflict detection is allowed for the given namespace.
// Returns true if under budget, false if budget exceeded.
func (t *ConflictBudgetTracker) Allow(ns string, limit int) bool {
	if limit <= 0 {
		return true // unlimited
	}

	t.mu.Lock()
	c, ok := t.counters[ns]
	if !ok {
		c = &budgetCounter{limit: limit, resetAt: time.Now().Add(time.Second)}
		t.counters[ns] = c
	}
	t.mu.Unlock()

	now := time.Now()
	if now.After(c.resetAt) {
		c.count.Store(0)
		c.resetAt = now.Add(time.Second)
	}

	return c.count.Add(1) <= int64(limit)
}
