package federation_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/federation"
	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/internal/store/memory"
)

const localPeerID = "peer-local"
const remotePeerID = "peer-remote"

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func newApplier(t *testing.T) (*federation.Applier, store.GraphStore, store.EventLog) {
	t.Helper()
	graph := memory.NewGraphStore()
	log := memory.NewEventLog()
	a := federation.NewApplier(graph, nil, log, localPeerID)
	return a, graph, log
}

// TestApplier_NodeUpsert_NewNode verifies that a remote node that doesn't
// exist locally is applied.
func TestApplier_NodeUpsert_NewNode(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	a, graph, _ := newApplier(t)

	nodeID := uuid.New()
	remote := core.Node{
		ID:         nodeID,
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "hello"},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(-time.Hour),
		TxTime:     time.Now(),
		Version:    1,
	}

	evt := store.Event{
		ID:        uuid.New(),
		Namespace: "test",
		Type:      store.EventNodeUpsert,
		Payload:   mustMarshal(t, remote),
		TxTime:    remote.TxTime,
		Origin:    remotePeerID,
	}

	applied, err := a.ApplyBatch(ctx, []store.Event{evt})
	is.NoErr(err)
	is.Equal(applied, 1)

	got, err := graph.GetNode(ctx, "test", nodeID)
	is.NoErr(err)
	is.True(got != nil)
	is.Equal(got.ID, nodeID)
}

// TestApplier_NodeUpsert_LWW_HigherVersion verifies that a remote node with
// a higher version than the local node is applied (remote wins).
func TestApplier_NodeUpsert_LWW_HigherVersion(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	a, graph, _ := newApplier(t)

	nodeID := uuid.New()

	// Insert local version 1.
	localNode := core.Node{
		ID:         nodeID,
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "local v1"},
		Confidence: 0.5,
		ValidFrom:  time.Now().Add(-2 * time.Hour),
		TxTime:     time.Now().Add(-2 * time.Hour),
	}
	is.NoErr(graph.UpsertNode(ctx, localNode))

	// Local node now has Version=1 (auto-set by memory store).
	local, err := graph.GetNode(ctx, "test", nodeID)
	is.NoErr(err)
	is.Equal(local.Version, uint64(1))

	// Remote has Version=2 (higher) → should win.
	remoteNode := core.Node{
		ID:         nodeID,
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "remote v2"},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(-time.Hour),
		TxTime:     time.Now(),
		Version:    2,
	}

	evt := store.Event{
		ID:        uuid.New(),
		Namespace: "test",
		Type:      store.EventNodeUpsert,
		Payload:   mustMarshal(t, remoteNode),
		TxTime:    remoteNode.TxTime,
		Origin:    remotePeerID,
	}

	applied, err := a.ApplyBatch(ctx, []store.Event{evt})
	is.NoErr(err)
	is.Equal(applied, 1)
}

// TestApplier_NodeUpsert_LWW_LowerVersion verifies that a remote node with
// a lower version than the local node is skipped (local wins).
func TestApplier_NodeUpsert_LWW_LowerVersion(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	a, graph, _ := newApplier(t)

	nodeID := uuid.New()

	// Insert local versions 1 and 2 so GetNode returns Version=2.
	for i := 0; i < 2; i++ {
		is.NoErr(graph.UpsertNode(ctx, core.Node{
			ID:         nodeID,
			Namespace:  "test",
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "local"},
			Confidence: 0.8,
			ValidFrom:  time.Now().Add(-time.Hour),
			TxTime:     time.Now(),
		}))
	}

	local, err := graph.GetNode(ctx, "test", nodeID)
	is.NoErr(err)
	is.Equal(local.Version, uint64(2))

	// Remote has Version=1 (lower) → should be skipped.
	remoteNode := core.Node{
		ID:         nodeID,
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "remote stale"},
		Confidence: 0.3,
		ValidFrom:  time.Now().Add(-3 * time.Hour),
		TxTime:     time.Now().Add(-3 * time.Hour),
		Version:    1,
	}

	evt := store.Event{
		ID:        uuid.New(),
		Namespace: "test",
		Type:      store.EventNodeUpsert,
		Payload:   mustMarshal(t, remoteNode),
		TxTime:    remoteNode.TxTime,
		Origin:    remotePeerID,
	}

	applied, err := a.ApplyBatch(ctx, []store.Event{evt})
	is.NoErr(err)
	is.Equal(applied, 0)
}

// TestApplier_SourceUpdate_Merge verifies that the Beta-space additive merge
// correctly combines local and remote credibility observations.
//
// Local:  Alpha=3, Beta=2
// Remote: Alpha=4, Beta=3
// Expected merged (subtract shared Beta(1,1) prior once):
//   Alpha = 3 + (4 - 1) = 6
//   Beta  = 2 + (3 - 1) = 4
func TestApplier_SourceUpdate_Merge(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	a, graph, _ := newApplier(t)

	// Seed local source with Alpha=3, Beta=2.
	localSrc := core.Source{
		ID:         uuid.New(),
		Namespace:  "test",
		ExternalID: "agent:bob",
		Alpha:      3,
		Beta:       2,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	is.NoErr(graph.UpsertSource(ctx, localSrc))

	// Remote source with Alpha=4, Beta=3.
	remoteSrc := core.Source{
		ID:         uuid.New(), // different ID — same ExternalID
		Namespace:  "test",
		ExternalID: "agent:bob",
		Alpha:      4,
		Beta:       3,
		UpdatedAt:  time.Now().Add(time.Second),
	}

	evt := store.Event{
		ID:        uuid.New(),
		Namespace: "test",
		Type:      store.EventSourceUpdate,
		Payload:   mustMarshal(t, remoteSrc),
		TxTime:    time.Now(),
		Origin:    remotePeerID,
	}

	applied, err := a.ApplyBatch(ctx, []store.Event{evt})
	is.NoErr(err)
	is.Equal(applied, 1)

	merged, err := graph.GetSourceByExternalID(ctx, "test", "agent:bob")
	is.NoErr(err)
	is.True(merged != nil)
	is.Equal(merged.Alpha, float64(6)) // 3 + (4-1)
	is.Equal(merged.Beta, float64(4))  // 2 + (3-1)
}

// TestApplier_SkipOwnOrigin verifies that events whose Origin matches the
// local peer ID are skipped to prevent echo replication.
func TestApplier_SkipOwnOrigin(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	a, graph, _ := newApplier(t)

	nodeID := uuid.New()
	remote := core.Node{
		ID:         nodeID,
		Namespace:  "test",
		Labels:     []string{"Claim"},
		Properties: map[string]any{"text": "echo"},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(-time.Hour),
		TxTime:     time.Now(),
		Version:    1,
	}

	evt := store.Event{
		ID:        uuid.New(),
		Namespace: "test",
		Type:      store.EventNodeUpsert,
		Payload:   mustMarshal(t, remote),
		TxTime:    remote.TxTime,
		Origin:    localPeerID, // same as our own peer ID
	}

	applied, err := a.ApplyBatch(ctx, []store.Event{evt})
	is.NoErr(err)
	is.Equal(applied, 0) // skipped

	got, err := graph.GetNode(ctx, "test", nodeID)
	is.NoErr(err)
	is.True(got == nil) // node was not written
}
