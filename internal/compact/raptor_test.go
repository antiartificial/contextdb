package compact

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/store"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
)

func TestClusterNodes(t *testing.T) {
	is := is.New(t)

	nodes := []core.Node{
		{ID: uuid.New(), Vector: []float32{1.0, 0.0, 0.0, 0.0}},
		{ID: uuid.New(), Vector: []float32{0.95, 0.05, 0.0, 0.0}},
		{ID: uuid.New(), Vector: []float32{0.9, 0.1, 0.0, 0.0}},
		{ID: uuid.New(), Vector: []float32{0.0, 0.0, 1.0, 0.0}},
		{ID: uuid.New(), Vector: []float32{0.0, 0.0, 0.95, 0.05}},
	}

	clusters := clusterNodes(nodes, 0.8, 2, 50)
	is.True(len(clusters) >= 1) // at least the first 3 should cluster
}

func TestAverageVector(t *testing.T) {
	is := is.New(t)

	nodes := []core.Node{
		{Vector: []float32{1.0, 0.0}},
		{Vector: []float32{0.0, 1.0}},
	}

	avg := averageVector(nodes)
	is.Equal(len(avg), 2)
	is.True(avg[0] > 0.49 && avg[0] < 0.51)
	is.True(avg[1] > 0.49 && avg[1] < 0.51)
}

func TestWorker_ProcessNamespace(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	eventLog := memstore.NewEventLog()

	// Create cluster of similar nodes
	ns := "test"
	nodeIDs := make([]uuid.UUID, 5)
	for i := 0; i < 5; i++ {
		id := uuid.New()
		nodeIDs[i] = id
		vec := []float32{0.9, float32(i) * 0.01, 0.0, 0.0}

		n := core.Node{
			ID: id, Namespace: ns,
			Labels:     []string{"Claim"},
			Properties: map[string]any{"text": "Go is a great language"},
			Vector:     vec,
			Confidence: 0.8,
			ValidFrom:  time.Now().Add(-time.Hour),
			TxTime:     time.Now(),
		}
		is.NoErr(graph.UpsertNode(ctx, n))
		vecs.RegisterNode(n)
		is.NoErr(vecs.Index(ctx, core.VectorEntry{
			ID: uuid.New(), Namespace: ns,
			NodeID: &id, Vector: vec,
		}))

		payload, _ := json.Marshal(map[string]any{"id": id.String()})
		is.NoErr(eventLog.Append(ctx, store.Event{
			Namespace: ns,
			Type:      store.EventNodeUpsert,
			Payload:   payload,
		}))
	}

	worker := NewWorker(graph, vecs, eventLog, nil, RaptorConfig{
		ClusterThreshold: 0.8,
		MinClusterSize:   3,
		MaxClusterSize:   50,
		Namespaces:       []string{ns},
	}, nil)

	err := worker.processNamespace(ctx, ns)
	is.NoErr(err)

	// Verify summary node was created (we can search for RAPTOR label)
	results, err := vecs.Search(ctx, store.VectorQuery{
		Namespace: ns,
		Vector:    []float32{0.9, 0.0, 0.0, 0.0},
		TopK:      20,
	})
	is.NoErr(err)
	is.True(len(results) > 5) // original 5 + summary node(s)
}

func TestFallbackSummary(t *testing.T) {
	is := is.New(t)

	is.Equal(fallbackSummary(nil), "")
	is.Equal(fallbackSummary([]string{"a"}), "a")
	is.Equal(fallbackSummary([]string{"a", "b"}), "a; b")

	s := fallbackSummary([]string{"a", "b", "c", "d", "e"})
	is.True(len(s) > 0)
}
