package federation

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// ─── shouldFederate tests ─────────────────────────────────────────────────────

func TestShouldFederate_EmptyNamespaces_FederatesAll(t *testing.T) {
	f := &Federation{config: Config{Namespaces: nil}}
	if !f.shouldFederate("anything") {
		t.Error("empty Namespaces should federate all namespaces")
	}
}

func TestShouldFederate_Wildcard(t *testing.T) {
	f := &Federation{config: Config{Namespaces: []string{"*"}}}
	for _, ns := range []string{"alpha", "beta", "prod"} {
		if !f.shouldFederate(ns) {
			t.Errorf("wildcard should match %q", ns)
		}
	}
}

func TestShouldFederate_SpecificNamespace_Match(t *testing.T) {
	f := &Federation{config: Config{Namespaces: []string{"prod", "staging"}}}
	if !f.shouldFederate("prod") {
		t.Error("expected shouldFederate(\"prod\") == true")
	}
	if !f.shouldFederate("staging") {
		t.Error("expected shouldFederate(\"staging\") == true")
	}
}

func TestShouldFederate_SpecificNamespace_NoMatch(t *testing.T) {
	f := &Federation{config: Config{Namespaces: []string{"prod"}}}
	if f.shouldFederate("dev") {
		t.Error("expected shouldFederate(\"dev\") == false when only \"prod\" is configured")
	}
}

func TestShouldFederate_MixedList_WildcardTakesPrecedence(t *testing.T) {
	f := &Federation{config: Config{Namespaces: []string{"prod", "*"}}}
	if !f.shouldFederate("anything") {
		t.Error("wildcard in mixed list should still match all namespaces")
	}
}

// ─── Replicator.Run noop tests ────────────────────────────────────────────────

// TestReplicator_Run_NoPeers verifies that the replicator starts and stops
// cleanly when there are no alive peers — no panics, no errors.
func TestReplicator_Run_NoPeers(t *testing.T) {
	f := &Federation{
		config:     Config{PullInterval: 10 * time.Millisecond},
		peers:      NewPeerRegistry(),
		watermarks: make(map[string]time.Time),
	}
	applier := NewApplier(nil, nil, nil, "test-peer")
	r := NewReplicator(f, applier, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Run must return cleanly when ctx is cancelled, without panicking.
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()

	select {
	case <-done:
		// success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Replicator.Run did not return after context cancellation")
	}
}

// TestReplicator_Run_MultipleTicksNoPanic verifies that multiple ticker ticks
// with no peers do not cause any panic or data race.
func TestReplicator_Run_MultipleTicksNoPanic(t *testing.T) {
	f := &Federation{
		config:     Config{PullInterval: 5 * time.Millisecond},
		peers:      NewPeerRegistry(),
		watermarks: make(map[string]time.Time),
	}
	applier := NewApplier(nil, nil, nil, "test-peer")
	r := NewReplicator(f, applier, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Let it tick a few times with no peers — must not panic.
	r.Run(ctx)
}
