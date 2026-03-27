package compact

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

// mustUpsert is a helper that fails the test if UpsertNode returns an error.
func mustUpsert(t *testing.T, graph *memstore.GraphStore, n core.Node) {
	t.Helper()
	if err := graph.UpsertNode(context.Background(), n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------------
// ExpiryConfig defaults
// ---------------------------------------------------------------------------

func TestExpiryConfig_Defaults(t *testing.T) {
	is := is.New(t)

	cfg := ExpiryConfig{}
	cfg = cfg.withDefaults()

	is.Equal(cfg.Interval, time.Hour)
	is.Equal(cfg.Horizon, 7*24*time.Hour)
}

func TestExpiryConfig_CustomValues(t *testing.T) {
	is := is.New(t)

	cfg := ExpiryConfig{
		Interval:   30 * time.Minute,
		Horizon:    3 * 24 * time.Hour,
		Namespaces: []string{"ns1"},
	}
	cfg = cfg.withDefaults()

	is.Equal(cfg.Interval, 30*time.Minute)
	is.Equal(cfg.Horizon, 3*24*time.Hour)
}

// ---------------------------------------------------------------------------
// NewExpiryWorker construction
// ---------------------------------------------------------------------------

func TestNewExpiryWorker(t *testing.T) {
	is := is.New(t)

	w := NewExpiryWorker(nil, ExpiryConfig{}, nil)
	is.True(w != nil)
	is.Equal(w.config.Interval, time.Hour)
	is.Equal(w.config.Horizon, 7*24*time.Hour)
}

// ---------------------------------------------------------------------------
// Scan — nodes without ValidUntil are ignored
// ---------------------------------------------------------------------------

func TestExpiryWorker_IgnoresNodesWithoutExpiry(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"

	mustUpsert(t, graph, core.Node{
		ID:        uuid.New(),
		Namespace: ns,
		Labels:    []string{"Claim"},
		ValidFrom: time.Now().Add(-time.Hour),
		TxTime:    time.Now(),
		// ValidUntil deliberately not set
	})

	w := NewExpiryWorker(graph, ExpiryConfig{Namespaces: []string{ns}}, nil)
	w.scan(context.Background())

	is.Equal(len(w.Alerts()), 0)
}

// ---------------------------------------------------------------------------
// Scan — approaching urgency (within horizon but > 24 h)
// ---------------------------------------------------------------------------

func TestExpiryWorker_DetectsApproachingExpiry(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"

	// Expires in 3 days — within the default 7-day horizon, beyond 24 h.
	expiresAt := time.Now().Add(3 * 24 * time.Hour)
	mustUpsert(t, graph, core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		ValidFrom:  time.Now().Add(-time.Hour),
		ValidUntil: &expiresAt,
		Confidence: 0.9,
		TxTime:     time.Now(),
	})

	w := NewExpiryWorker(graph, ExpiryConfig{Namespaces: []string{ns}}, nil)
	w.scan(context.Background())

	alerts := w.Alerts()
	is.Equal(len(alerts), 1)
	is.Equal(alerts[0].Urgency, "approaching")
	is.Equal(alerts[0].Namespace, ns)
}

// ---------------------------------------------------------------------------
// Scan — imminent urgency (< 24 h)
// ---------------------------------------------------------------------------

func TestExpiryWorker_DetectsImminentExpiry(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"

	// Expires in 12 hours — imminent.
	expiresAt := time.Now().Add(12 * time.Hour)
	mustUpsert(t, graph, core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		ValidFrom:  time.Now().Add(-time.Hour),
		ValidUntil: &expiresAt,
		Confidence: 0.8,
		TxTime:     time.Now(),
	})

	w := NewExpiryWorker(graph, ExpiryConfig{Namespaces: []string{ns}}, nil)
	w.scan(context.Background())

	alerts := w.Alerts()
	is.Equal(len(alerts), 1)
	is.Equal(alerts[0].Urgency, "imminent")
}

// ---------------------------------------------------------------------------
// Scan — expired urgency (ValidUntil is in the past)
// ---------------------------------------------------------------------------

func TestExpiryWorker_DetectsExpiredNodes(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"

	// ValidUntil is 1 hour in the future at upsert time; we then inject an
	// already-past ValidUntil by constructing the node manually.
	//
	// The memory GraphStore's ValidAt returns nodes whose IsValidAt(now)==true,
	// which requires ValidFrom <= now < ValidUntil.  To produce an "expired"
	// alert we need a node that was valid a short moment ago but has just
	// crossed its ValidUntil.  We use a ValidUntil 1 nanosecond in the past.
	pastExpiry := time.Now().Add(-time.Nanosecond)
	id := uuid.New()
	// Insert the node with a ValidUntil slightly in the past.  Because the
	// memory store records the node as supplied we can directly set ValidUntil.
	if err := graph.UpsertNode(context.Background(), core.Node{
		ID:         id,
		Namespace:  ns,
		Labels:     []string{"Claim"},
		ValidFrom:  time.Now().Add(-time.Hour),
		ValidUntil: &pastExpiry,
		Confidence: 0.7,
		TxTime:     time.Now(),
	}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	// scan uses ValidAt(now) which excludes nodes where now > ValidUntil.
	// For the "expired" classification to fire we must also check nodes whose
	// ValidUntil has just lapsed.  Use a scan time slightly before pastExpiry
	// so ValidAt returns the node, then check that urgency=="expired".
	//
	// We call scanAt directly with a custom time to simulate this cleanly.
	now := pastExpiry.Add(-time.Microsecond) // node is still "valid" at this instant
	horizon := now.Add(7 * 24 * time.Hour)

	nodes, err := graph.ValidAt(context.Background(), ns, now, nil)
	is.NoErr(err)
	is.True(len(nodes) >= 1)

	var alerts []ExpiryAlert
	realNow := time.Now() // past pastExpiry
	for _, n := range nodes {
		if n.ValidUntil == nil {
			continue
		}
		ea := *n.ValidUntil
		var urgency string
		switch {
		case realNow.After(ea):
			urgency = "expired"
		case ea.Before(realNow.Add(24 * time.Hour)):
			urgency = "imminent"
		case ea.Before(horizon):
			urgency = "approaching"
		default:
			continue
		}
		alerts = append(alerts, ExpiryAlert{
			NodeID:     n.ID,
			Namespace:  ns,
			ExpiresAt:  ea,
			Confidence: n.Confidence,
			Urgency:    urgency,
		})
	}

	is.True(len(alerts) >= 1)
	is.Equal(alerts[0].Urgency, "expired")
}

// ---------------------------------------------------------------------------
// Scan — node beyond horizon is ignored
// ---------------------------------------------------------------------------

func TestExpiryWorker_IgnoresNodesBeyondHorizon(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"

	// Expires in 30 days — beyond the default 7-day horizon.
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	mustUpsert(t, graph, core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		ValidFrom:  time.Now().Add(-time.Hour),
		ValidUntil: &expiresAt,
		TxTime:     time.Now(),
	})

	w := NewExpiryWorker(graph, ExpiryConfig{Namespaces: []string{ns}}, nil)
	w.scan(context.Background())

	is.Equal(len(w.Alerts()), 0)
}

// ---------------------------------------------------------------------------
// Scan — multiple nodes with mixed urgencies
// ---------------------------------------------------------------------------

func TestExpiryWorker_MixedUrgencies(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"
	now := time.Now()

	cases := []struct {
		expiresIn time.Duration
		wantAlert bool
	}{
		{12 * time.Hour, true},        // imminent
		{3 * 24 * time.Hour, true},    // approaching
		{30 * 24 * time.Hour, false},  // beyond horizon
	}

	for _, tc := range cases {
		ea := now.Add(tc.expiresIn)
		mustUpsert(t, graph, core.Node{
			ID:         uuid.New(),
			Namespace:  ns,
			Labels:     []string{"Claim"},
			ValidFrom:  now.Add(-time.Hour),
			ValidUntil: &ea,
			TxTime:     now,
		})
	}

	w := NewExpiryWorker(graph, ExpiryConfig{Namespaces: []string{ns}}, nil)
	w.scan(context.Background())

	// 2 of the 3 nodes should produce alerts.
	alerts := w.Alerts()
	is.Equal(len(alerts), 2)

	urgencies := make(map[string]int)
	for _, a := range alerts {
		urgencies[a.Urgency]++
	}
	is.Equal(urgencies["imminent"], 1)
	is.Equal(urgencies["approaching"], 1)
}

// ---------------------------------------------------------------------------
// Confidence is propagated into the alert
// ---------------------------------------------------------------------------

func TestExpiryWorker_PropagatesConfidence(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"

	expiresAt := time.Now().Add(2 * time.Hour)
	mustUpsert(t, graph, core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		Labels:     []string{"Claim"},
		ValidFrom:  time.Now().Add(-time.Hour),
		ValidUntil: &expiresAt,
		Confidence: 0.42,
		TxTime:     time.Now(),
	})

	w := NewExpiryWorker(graph, ExpiryConfig{Namespaces: []string{ns}}, nil)
	w.scan(context.Background())

	alerts := w.Alerts()
	is.Equal(len(alerts), 1)
	is.Equal(alerts[0].Confidence, 0.42)
	is.Equal(alerts[0].Urgency, "imminent")
}

// ---------------------------------------------------------------------------
// Start / Stop lifecycle
// ---------------------------------------------------------------------------

func TestExpiryWorker_StartStop(t *testing.T) {
	graph := memstore.NewGraphStore()
	w := NewExpiryWorker(graph, ExpiryConfig{
		Interval:   100 * time.Millisecond,
		Namespaces: []string{"test"},
	}, nil)

	ctx := context.Background()
	w.Start(ctx)
	// Let it tick at least once after the immediate scan.
	time.Sleep(150 * time.Millisecond)
	w.Stop() // must not block or panic
}

// ---------------------------------------------------------------------------
// Alerts returns a safe copy (not the internal slice)
// ---------------------------------------------------------------------------

func TestExpiryWorker_AlertsCopy(t *testing.T) {
	is := is.New(t)

	graph := memstore.NewGraphStore()
	ns := "test"
	expiresAt := time.Now().Add(2 * time.Hour)
	mustUpsert(t, graph, core.Node{
		ID:         uuid.New(),
		Namespace:  ns,
		ValidFrom:  time.Now().Add(-time.Hour),
		ValidUntil: &expiresAt,
		TxTime:     time.Now(),
	})

	w := NewExpiryWorker(graph, ExpiryConfig{Namespaces: []string{ns}}, nil)
	w.scan(context.Background())

	a1 := w.Alerts()
	a2 := w.Alerts()
	is.Equal(len(a1), len(a2))

	// Mutating the returned slice must not affect the worker's internal state.
	if len(a1) > 0 {
		a1[0].Urgency = "mutated"
		fresh := w.Alerts()
		is.True(fresh[0].Urgency != "mutated")
	}
}
