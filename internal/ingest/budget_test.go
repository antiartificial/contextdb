package ingest

import (
	"testing"
)

func TestConflictBudgetTracker_UnderBudget(t *testing.T) {
	tracker := NewConflictBudgetTracker()
	const limit = 5
	for i := 0; i < limit; i++ {
		if !tracker.Allow("ns1", limit) {
			t.Fatalf("expected Allow=true on call %d (limit=%d)", i+1, limit)
		}
	}
}

func TestConflictBudgetTracker_ExceedBudget(t *testing.T) {
	tracker := NewConflictBudgetTracker()
	const limit = 3
	// Exhaust the budget.
	for i := 0; i < limit; i++ {
		tracker.Allow("ns1", limit)
	}
	// The very next call should be denied.
	if tracker.Allow("ns1", limit) {
		t.Fatal("expected Allow=false after budget exhausted")
	}
}

func TestConflictBudgetTracker_Unlimited(t *testing.T) {
	tracker := NewConflictBudgetTracker()
	// limit=0 means unlimited; should always return true.
	for i := 0; i < 1000; i++ {
		if !tracker.Allow("ns1", 0) {
			t.Fatalf("expected Allow=true for unlimited budget on call %d", i+1)
		}
	}
}

func TestConflictBudgetTracker_IndependentNamespaces(t *testing.T) {
	tracker := NewConflictBudgetTracker()
	const limit = 2

	// Exhaust ns1.
	tracker.Allow("ns1", limit)
	tracker.Allow("ns1", limit)

	// ns2 should still be allowed.
	if !tracker.Allow("ns2", limit) {
		t.Fatal("expected Allow=true for ns2 after ns1 was exhausted")
	}
}
