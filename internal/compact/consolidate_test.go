package compact

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/store"
	"github.com/antiartificial/contextdb/internal/store/memory"
	"github.com/google/uuid"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/core"
	"github.com/antiartificial/contextdb/internal/extract"
)

type supportProvider struct{}

func (supportProvider) Chat(context.Context, extract.ChatRequest) (*extract.ChatResponse, error) {
	return &extract.ChatResponse{Content: "FACT: recurring launch date | SOURCES: 1,3\nFACT: unsupported | SOURCES: 2"}, nil
}
func (supportProvider) Embed(context.Context, []string, string) ([][]float32, error) { return nil, nil }

func TestConsolidationUsesClaimSpecificSupport(t *testing.T) {
	c := NewConsolidator(nil, nil, nil, supportProvider{}, ConsolidationConfig{}, nil)
	nodes := []core.Node{
		{ID: uuid.New(), Properties: map[string]any{"text": "first"}, Confidence: .8},
		{ID: uuid.New(), Properties: map[string]any{"text": "unrelated"}, Confidence: .2},
		{ID: uuid.New(), Properties: map[string]any{"text": "third"}, Confidence: .9},
	}
	facts, err := c.extractFacts(context.Background(), nodes)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts: %v %+v", err, facts)
	}
	if len(facts[0].SourceNodeIDs) != 2 || facts[0].SourceNodeIDs[0] != nodes[0].ID || facts[0].SourceNodeIDs[1] != nodes[2].ID {
		t.Fatalf("support IDs: %+v", facts[0].SourceNodeIDs)
	}
}

func TestConsolidationRetainsYoungEventsAcrossWorkerRestart(t *testing.T) {
	ctx := context.Background()
	graph, vectors, log := memory.NewGraphStore(), memory.NewVectorIndex(), memory.NewEventLog()
	ns, now := "test:consolidation-cursor", time.Now()
	for i := 0; i < 3; i++ {
		id := uuid.New()
		age := 3 * time.Hour
		if i == 2 {
			age = time.Hour
		}
		node := core.Node{ID: id, Namespace: ns, Labels: []string{"episodic"}, Properties: map[string]any{"text": "same event"}, ValidFrom: now.Add(-age), TxTime: now.Add(-age)}
		if err := graph.UpsertNode(ctx, node); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]any{"id": id})
		if err := log.Append(ctx, store.Event{ID: uuid.New(), Namespace: ns, Type: store.EventNodeUpsert, Payload: payload, TxTime: now.Add(-age)}); err != nil {
			t.Fatal(err)
		}
	}
	first := NewConsolidator(graph, vectors, log, nil, ConsolidationConfig{Namespaces: []string{ns}, AgeThreshold: 2 * time.Hour, FrequencyThreshold: 3}, nil)
	if err := first.processNamespace(ctx, ns); err != nil {
		t.Fatal(err)
	}
	pending, err := log.Since(ctx, ns, time.Unix(0, 0))
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending after young skip: %v %d", err, len(pending))
	}
	second := NewConsolidator(graph, vectors, log, nil, ConsolidationConfig{Namespaces: []string{ns}, AgeThreshold: 30 * time.Minute, FrequencyThreshold: 3}, nil)
	if err := second.processNamespace(ctx, ns); err != nil {
		t.Fatal(err)
	}
	pending, err = log.Since(ctx, ns, time.Unix(0, 0))
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after retry: %v %d", err, len(pending))
	}
	derived, err := graph.ValidAt(ctx, ns, now.Add(time.Second), []string{"Consolidated"})
	if err != nil || len(derived) != 1 {
		t.Fatalf("derived result: %v %+v", err, derived)
	}
	support, ok := derived[0].Properties["support_versions"].([]map[string]any)
	if !ok || len(support) != 3 {
		t.Fatalf("support lineage: %+v", derived[0].Properties["support_versions"])
	}
}

func TestConsolidationConfig_Defaults(t *testing.T) {
	is := is.New(t)

	cfg := ConsolidationConfig{}
	cfg = cfg.withDefaults()

	is.Equal(cfg.AgeThreshold, 24*time.Hour)
	is.Equal(cfg.FrequencyThreshold, 3)
	is.Equal(cfg.Interval, 30*time.Minute)
}

func TestConsolidationConfig_CustomValues(t *testing.T) {
	is := is.New(t)

	cfg := ConsolidationConfig{
		AgeThreshold:       12 * time.Hour,
		FrequencyThreshold: 5,
		Interval:           10 * time.Minute,
		Namespaces:         []string{"ns1", "ns2"},
	}
	cfg = cfg.withDefaults()

	is.Equal(cfg.AgeThreshold, 12*time.Hour)
	is.Equal(cfg.FrequencyThreshold, 5)
	is.Equal(cfg.Interval, 10*time.Minute)
	is.Equal(len(cfg.Namespaces), 2)
}

func TestNewConsolidator(t *testing.T) {
	is := is.New(t)

	c := NewConsolidator(nil, nil, nil, nil, ConsolidationConfig{}, nil)
	is.True(c != nil)
	is.Equal(c.config.AgeThreshold, 24*time.Hour)
	is.Equal(c.config.FrequencyThreshold, 3)
	is.Equal(c.config.Interval, 30*time.Minute)
}

func TestIsEpisodicMemory(t *testing.T) {
	is := is.New(t)

	// Test with label
	node := &core.Node{
		Labels: []string{"episodic"},
	}
	is.True(isEpisodicMemory(node))

	// Test with Episode label
	node2 := &core.Node{
		Labels: []string{"Episode"},
	}
	is.True(isEpisodicMemory(node2))

	// Test with memory_type property
	node3 := &core.Node{
		Properties: map[string]any{"memory_type": "episodic"},
	}
	is.True(isEpisodicMemory(node3))

	// Test non-episodic
	node4 := &core.Node{
		Labels: []string{"semantic"},
	}
	is.True(!isEpisodicMemory(node4))
}
